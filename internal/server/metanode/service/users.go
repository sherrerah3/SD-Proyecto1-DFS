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

// Límites de validación de credenciales.
const (
	minUsernameLen = 1
	maxUsernameLen = 16
	minPasswordLen = 6
	maxPasswordLen = 72
)

// TTL por defecto del JWT de sesión (el capability token usa su propio TTL).
const defaultSessionTTL = 5 * time.Minute

// Nombre de la variable de entorno con la clave de firma de los JWT.
const jwtSecretEnvVar = "METANODE_JWT_SECRET"

// Errores tipados para que las capas superiores los mapeen a códigos HTTP.
var (
	ErrInvalidUsername    = errors.New("username inválido: 1 a 16 caracteres alfanuméricos")
	ErrInvalidPassword    = errors.New("password inválida: 6 a 72 caracteres alfanuméricos")
	ErrUserAlreadyExists  = errors.New("el usuario ya existe")
	ErrInvalidCredentials = errors.New("usuario o contraseña incorrectos")
	ErrMissingJWTSecret   = errors.New("falta la variable de entorno " + jwtSecretEnvVar)
	ErrInvalidToken       = errors.New("token inválido o expirado")
)

// Register valida credenciales, hashea con bcrypt y crea el usuario con cuota en 0.
func Register(s store.Store, username, plaintextPassword string) error {
	if !validUsername(username) {
		return ErrInvalidUsername
	}

	if !validPassword(plaintextPassword) {
		return ErrInvalidPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("no se pudo hashear la contraseña: %w", err)
	}

	// CreateUser verifica el duplicado bajo su propio lock; crear directo evita el TOCTOU.
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

// Login verifica las credenciales y devuelve un JWT de sesión si son correctas.
func Login(s store.Store, username, plaintextPassword string) (string, error) {
	user, err := s.GetUser(username)
	if err != nil {
		// Mismo error para usuario inexistente y password mala: no filtrar cuáles usuarios existen.
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(plaintextPassword)); err != nil {
		return "", ErrInvalidCredentials
	}

	return generateSessionToken(username, defaultSessionTTL)
}

// ValidateJWT verifica la firma y expiración de un JWT de sesión y devuelve el username.
func ValidateJWT(tokenString string) (string, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return "", err
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return "", ErrInvalidToken
	}

	return sub, nil
}

// GenerateCapabilityToken emite un JWT de vida corta que autoriza bloques ante los DataNodes.
func GenerateCapabilityToken(username string, blockIDs []string, ttl time.Duration) (string, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       username,
		"block_ids": blockIDs,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}

// ValidateCapabilityToken verifica el token y devuelve el username y los bloques autorizados.
func ValidateCapabilityToken(tokenString string) (string, []string, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return "", nil, err
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return "", nil, ErrInvalidToken
	}

	rawBlocks, ok := claims["block_ids"].([]any)
	if !ok {
		return "", nil, ErrInvalidToken
	}

	blockIDs := make([]string, 0, len(rawBlocks))
	for _, item := range rawBlocks {
		id, ok := item.(string)
		if !ok {
			return "", nil, ErrInvalidToken
		}
		blockIDs = append(blockIDs, id)
	}

	return sub, blockIDs, nil
}

// --- helpers internos ---

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
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}

func parseToken(tokenString string) (jwt.MapClaims, error) {
	secret, err := jwtSecret()
	if err != nil {
		return nil, err
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Rechazar cualquier algoritmo que no sea el HMAC que usamos al firmar.
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
