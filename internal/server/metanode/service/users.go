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

// SessionTTL es la duración del JWT de sesión (el capability token usa su propio TTL).
const SessionTTL = 5 * time.Minute

// Variables de entorno con las claves de firma (sesión y capacidades son distintas)
const (
	jwtSecretEnvVar        = "METANODE_JWT_SECRET"
	capabilitySecretEnvVar = "DFSHA_CAPABILITY_SECRET"
)

// Tipos de token: distinguen un JWT de sesión de un capability token.
const (
	tokenTypeSession    = "session"
	tokenTypeCapability = "capability"
)

// Operaciones que puede autorizar un capability token.
const (
	OpPut = "put"
	OpGet = "get"
)

// MaxUsers es el tope de usuarios registrados
const MaxUsers = 50

// ErrTooManyUsers se devuelve al alcanzar el tope
var ErrTooManyUsers = errors.New("se alcanzó el máximo de usuarios registrados")

// hashDummy iguala el tiempo de login cuando el usuario no existe
var hashDummy = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// Errores tipados para que las capas superiores los mapeen a códigos HTTP.
var (
	ErrInvalidUsername         = errors.New("username inválido: 1 a 16 caracteres alfanuméricos")
	ErrInvalidPassword         = errors.New("password inválida: 6 a 72 caracteres alfanuméricos")
	ErrUserAlreadyExists       = errors.New("el usuario ya existe")
	ErrInvalidCredentials      = errors.New("usuario o contraseña incorrectos")
	ErrMissingJWTSecret        = errors.New("falta la variable de entorno " + jwtSecretEnvVar)
	ErrMissingCapabilitySecret = errors.New("falta la variable de entorno " + capabilitySecretEnvVar)
	ErrInvalidToken            = errors.New("token inválido o expirado")
)

// Valida credenciales, hashea con bcrypt y crea el usuario con cuota en 0.
func Register(s store.Store, username, plaintextPassword string) error {
	if !validUsername(username) {
		return ErrInvalidUsername
	}

	if !validPassword(plaintextPassword) {
		return ErrInvalidPassword
	}

	// Tope de usuarios
	if s.CountUsers() >= MaxUsers {
		return ErrTooManyUsers
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("no se pudo hashear la contraseña: %w", err)
	}

	// CreateUser verifica el duplicado bajo su propio lock
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

// Verifica las credenciales y devuelve un JWT de sesión si son correctas.
func Login(s store.Store, username, plaintextPassword string) (string, error) {
	user, err := s.GetUser(username)
	if err != nil {
		// Usuario inexistente: mismo costo que una contraseña mala.
		_ = bcrypt.CompareHashAndPassword(hashDummy, []byte(plaintextPassword))
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(plaintextPassword)); err != nil {
		return "", ErrInvalidCredentials
	}

	return generateSessionToken(username, SessionTTL)
}

// Verifica la firma y expiración de un JWT de sesión y devuelve el username.
func ValidateJWT(tokenString string) (string, error) {
	claims, err := parseToken(tokenString)
	if err != nil {
		return "", err
	}

	// Un capability token no debe servir como token de sesión.
	if err := requireTokenType(claims, tokenTypeSession); err != nil {
		return "", err
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return "", ErrInvalidToken
	}

	return sub, nil
}

// Capability es lo que autoriza un capability token validado
type Capability struct {
	Username string
	Op       string // OpPut u OpGet
	BlockIDs []string
}

// Emite un JWT de vida corta que autoriza una operación sobre bloques ante los DataNodes.
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

// Verifica un capability token y devuelve lo que autoriza.
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
		"typ": tokenTypeSession,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}

// requireTokenType rechaza el token si su claim "typ" no es el esperado.
func requireTokenType(claims jwt.MapClaims, want string) error {
	if typ, _ := claims["typ"].(string); typ != want {
		return ErrInvalidToken
	}
	return nil
}

func parseToken(tokenString string) (jwt.MapClaims, error) {
	return parseTokenWith(tokenString, jwtSecret)
}

// parseTokenWith valida la firma con la clave de secretFn
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

// RequireSecrets verifica al arrancar que existan las claves de firma
func RequireSecrets() error {
	if os.Getenv(jwtSecretEnvVar) == "" {
		return ErrMissingJWTSecret
	}
	if os.Getenv(capabilitySecretEnvVar) == "" {
		return ErrMissingCapabilitySecret
	}
	return nil
}
