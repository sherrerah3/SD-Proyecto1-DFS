// internal/metanode/https/server.go
package https

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"dfsha/common"
	dbconfig "dfsha/internal/metanode/db"

	"golang.org/x/crypto/bcrypt"
)

const tokenLifetime = 24 * time.Hour

// Shared by all registration handlers in this Go process.
var registerMu sync.Mutex

func NewServer(addr string, database *sql.DB) *http.Server {

	if err := dbconfig.InitSchema(database); err != nil {
		log.Fatalf("could not initialize database schema: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST "+common.LoginPath, func(w http.ResponseWriter, r *http.Request) {
		login(w, r, database)
	})

	mux.HandleFunc("POST "+common.RegisterPath, func(w http.ResponseWriter, r *http.Request) {
		register(w, r, database)
	})

	// Validates the saved token and returns the associated username.
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		getSession(w, r, database)
	})

	registerFilesystemRoutes(mux, database)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}

func login(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	var req common.AuthRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if !common.ValidAuthRequest(req) {
		http.Error(w, "invalid user or password format", http.StatusBadRequest)
		return
	}

	var userID int64
	var passwordHash string

	err := database.QueryRow(
		`SELECT user_id, password_hash
		 FROM users
		 WHERE username = ?`,
		req.User,
	).Scan(&userID, &passwordHash)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(req.Password),
	); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, tokenHash, err := generateToken()
	if err != nil {
		http.Error(w, "could not generate token", http.StatusInternalServerError)
		return
	}

	if err := saveSession(database, userID, tokenHash); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}

	writeToken(w, http.StatusOK, token)
}

func register(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	var req common.AuthRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if !common.ValidAuthRequest(req) {
		http.Error(w, "invalid user or password format", http.StatusBadRequest)
		return
	}

	// Serialize registrations so concurrent requests cannot exceed MaxUsers.
	registerMu.Lock()
	defer registerMu.Unlock()

	var count int

	if err := database.QueryRow(
		`SELECT COUNT(*) FROM users`,
	).Scan(&count); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if count >= dbconfig.MaxUsers {
		http.Error(w, "user limit reached", http.StatusConflict)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	result, err := database.Exec(
		`INSERT INTO users (username, password_hash)
		 VALUES (?, ?)`,
		req.User,
		string(passwordHash),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "username already exists", http.StatusConflict)
			return
		}

		http.Error(w, "could not create user", http.StatusInternalServerError)
		return
	}

	userID, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "could not get user ID", http.StatusInternalServerError)
		return
	}

	token, tokenHash, err := generateToken()
	if err != nil {
		http.Error(w, "could not generate token", http.StatusInternalServerError)
		return
	}

	if err := saveSession(database, userID, tokenHash); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}

	writeToken(w, http.StatusCreated, token)
}

// getSession validates the bearer token and returns the username associated
// with that token. The raw token is never stored in the database.
func getSession(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	const bearerPrefix = "Bearer "

	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, bearerPrefix) {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	token := strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	var username string

	err := database.QueryRow(
		`SELECT users.username
		 FROM sessions
		 INNER JOIN users ON users.user_id = sessions.user_id
		 WHERE sessions.token_hash = ?
		   AND sessions.expires_at > ?`,
		tokenHash,
		time.Now().Unix(),
	).Scan(&username)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}

		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(common.SessionInfoResponse{
		User: username,
	})
}

func generateToken() (string, string, error) {
	randomBytes := make([]byte, 32)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", err
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(token))

	return token, hex.EncodeToString(hash[:]), nil
}

func saveSession(database *sql.DB, userID int64, tokenHash string) error {
	_, err := database.Exec(
		`INSERT INTO sessions (token_hash, user_id, expires_at)
		 VALUES (?, ?, ?)`,
		tokenHash,
		userID,
		time.Now().Add(tokenLifetime).Unix(),
	)

	return err
}

func writeToken(w http.ResponseWriter, status int, token string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(common.AuthResponse{
		Token: token,
	})
}

// ValidateToken remains available for other server-side callers.
func ValidateToken(database *sql.DB, token string) (bool, error) {
	if token == "" {
		return false, nil
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	var userID int64

	err := database.QueryRow(
		`SELECT user_id
		 FROM sessions
		 WHERE token_hash = ?
		   AND expires_at > ?`,
		tokenHash,
		time.Now().Unix(),
	).Scan(&userID)

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}
