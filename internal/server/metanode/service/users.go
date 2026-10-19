package service

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"dfsha/internal/server/metanode/store"
)

// Credential validation limits.
const (
	minUsernameLen = 1
	maxUsernameLen = 16
	minPasswordLen = 6
	maxPasswordLen = 72
)

// SessionTTL is the session JWT lifetime (the capability token has its own TTL).
const SessionTTL = 5 * time.Minute

// Environment variables for signing keys (session and capability keys are different).
const (
	jwtSecretEnvVar        = "METANODE_JWT_SECRET"
	capabilitySecretEnvVar = "DFSHA_CAPABILITY_SECRET"
)

// Token types: distinguish a session JWT from a capability token.
const (
	tokenTypeSession    = "session"
	tokenTypeCapability = "capability"
)

// Operations that a capability token can authorize.
const (
	OpPut = "put"
	OpGet = "get"
)

// MaxUsers is the maximum number of registered users.
const MaxUsers = 50

// ErrTooManyUsers is returned when the user limit is reached.
var ErrTooManyUsers = errors.New("maximum number of registered users reached")

// hashDummy equalizes login processing time when the user does not exist.
var hashDummy = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// Typed errors so upper layers can map them to HTTP status codes.
var (
	ErrInvalidUsername         = errors.New("invalid username: 1 to 16 alphanumeric characters")
	ErrInvalidPassword         = errors.New("invalid password: 6 to 72 alphanumeric characters")
	ErrUserAlreadyExists       = errors.New("user already exists")
	ErrInvalidCredentials      = errors.New("incorrect username or password")
	ErrMissingJWTSecret        = errors.New("missing environment variable " + jwtSecretEnvVar)
	ErrMissingCapabilitySecret = errors.New("missing environment variable " + capabilitySecretEnvVar)
	ErrInvalidToken            = errors.New("invalid or expired token")
)

// Validates credentials, hashes the password with bcrypt, and creates the user with a quota of 0.
func Register(s store.Store, username, plaintextPassword string) error {
	if !validUsername(username) {
		return ErrInvalidUsername
	}

	if !validPassword(plaintextPassword) {
		return ErrInvalidPassword
	}

	// User limit.
	if s.CountUsers() >= MaxUsers {
		return ErrTooManyUsers
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// CreateUser checks for duplicates under its own lock.
	if err := s.CreateUser(store.UserInfo{
		Username:       username,
		PasswordHash:   string(hash),
		QuotaUsedBytes: 0,
	}); err != nil {
		if _, getErr := s.GetUser(username); getErr == nil {
			return ErrUserAlreadyExists
		}
		return err
	}

	return nil
}

// Verifies credentials and returns a session JWT if they are correct.
func Login(s store.Store, username, plaintextPassword string) (string, error) {
	user, err := s.GetUser(username)
	if err != nil {
		// Nonexistent user: use the same cost as an incorrect password.
		_ = bcrypt.CompareHashAndPassword(hashDummy, []byte(plaintextPassword))
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(plaintextPassword)); err != nil {
		return "", ErrInvalidCredentials
	}

	return generateSessionToken(username, SessionTTL)
}

// Verifies the signature and expiration of a session JWT and returns the username.
func ValidateJWT(tokenString string) (string, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return "", err
	}

	// A capability token must not be accepted as a session token.
	if err := requireTokenType(claims, tokenTypeSession); err != nil {
		return "", err
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return "", ErrInvalidToken
	}

	return sub, nil
}

// Capability represents what a validated capability token authorizes.
type Capability struct {
	Username string
	Op       string // OpPut or OpGet
	BlockIDs []string
}

// Issues a short-lived JWT authorizing an operation on blocks at the DataNodes.
func GenerateCapabilityToken(username, op string, blockIDs []string, ttl time.Duration) (string, error) {
	secret, err := capabilitySecret()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       username,
		"typ":       tokenTypeCapability,
		"op":        op,
		"block_ids": blockIDs,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}

// Verifies a capability token and returns the permissions it grants.
func ValidateCapabilityToken(tokenString string) (Capability, error) {
	claims, err := parseTokenWith(tokenString, capabilitySecret)
	if err != nil {
		return Capability{}, err
	}

	if err := requireTokenType(claims, tokenTypeCapability); err != nil {
		return Capability{}, err
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return Capability{}, ErrInvalidToken
	}

	op, _ := claims["op"].(string)
	if op != OpPut && op != OpGet {
		return Capability{}, ErrInvalidToken
	}

	rawBlocks, ok := claims["block_ids"].([]any)
	if !ok {
		return Capability{}, ErrInvalidToken
	}

	blockIDs := make([]string, 0, len(rawBlocks))
	for _, item := range rawBlocks {
		id, ok := item.(string)
		if !ok {
			return Capability{}, ErrInvalidToken
		}
		blockIDs = append(blockIDs, id)
	}

	return Capability{Username: sub, Op: op, BlockIDs: blockIDs}, nil
}

// --- Internal helpers ---

func validUsername(username string) bool {
	if len(username) < minUsernameLen || len(username) > maxUsernameLen {
		return false
	}
	return isAlphanumeric(username)
}

func validPassword(password string) bool {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return false
	}
	return isAlphanumeric(password)
}

func isAlphanumeric(value string) bool {
	for _, r := range value {
		isDigit := r >= '0' && r <= '9'
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

func generateSessionToken(username string, ttl time.Duration) (string, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub": username,
		"typ": tokenTypeSession,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}

// requireTokenType rejects the token if its "typ" claim does not match the expected value.
func requireTokenType(claims jwt.MapClaims, want string) error {
	if typ, _ := claims["typ"].(string); typ != want {
		return ErrInvalidToken
	}
	return nil
}

func parseToken(tokenString string) (jwt.MapClaims, error) {
	return parseTokenWith(tokenString, jwtSecret)
}

// parseTokenWith validates the signature using the key returned by secretFn.
func parseTokenWith(tokenString string, secretFn func() ([]byte, error)) (jwt.MapClaims, error) {
	secret, err := secretFn()
	if err != nil {
		return nil, err
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func jwtSecret() ([]byte, error) {
	secret := os.Getenv(jwtSecretEnvVar)
	if secret == "" {
		return nil, ErrMissingJWTSecret
	}
	return []byte(secret), nil
}

func capabilitySecret() ([]byte, error) {
	secret := os.Getenv(capabilitySecretEnvVar)
	if secret == "" {
		return nil, ErrMissingCapabilitySecret
	}
	return []byte(secret), nil
}

// RequireSecrets checks at startup that the signing keys are configured.
func RequireSecrets() error {
	if os.Getenv(jwtSecretEnvVar) == "" {
		return ErrMissingJWTSecret
	}
	if os.Getenv(capabilitySecretEnvVar) == "" {
		return ErrMissingCapabilitySecret
	}
	return nil
}
