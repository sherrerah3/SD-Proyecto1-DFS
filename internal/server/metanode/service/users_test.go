package service

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"dfsha/internal/server/metanode/store"
)

const testSecret = "test-secret-para-pruebas"

func TestRegisterValid(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("valid registration failed: %v", err)
	}

	u, err := s.GetUser("ana")
	if err != nil {
		t.Fatalf("user was not saved in the store: %v", err)
	}
	if u.PasswordHash == "" || u.PasswordHash == "secreta1" {
		t.Fatalf("password was not hashed: %q", u.PasswordHash)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	if err := Register(s, "ana", "secreta1"); err != ErrUserAlreadyExists {
		t.Fatalf("second registration: error = %v, expected ErrUserAlreadyExists", err)
	}
}

func TestRegisterInvalidUsername(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	cases := map[string]string{
		"empty":    "",
		"too long": strings.Repeat("a", 17),
	}

	for name, username := range cases {
		t.Run(name, func(t *testing.T) {
			s := store.NewMemoryStore()
			if err := Register(s, username, "secreta1"); err != ErrInvalidUsername {
				t.Fatalf("error = %v, expected ErrInvalidUsername", err)
			}
		})
	}
}

func TestRegisterInvalidPassword(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	cases := map[string]string{
		"too short": "abc",
		"too long":  strings.Repeat("a", 73),
	}

	for name, password := range cases {
		t.Run(name, func(t *testing.T) {
			s := store.NewMemoryStore()
			if err := Register(s, "ana", password); err != ErrInvalidPassword {
				t.Fatalf("error = %v, expected ErrInvalidPassword", err)
			}
		})
	}
}

func TestLoginValid(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	token, err := Login(s, "ana", "secreta1")
	if err != nil {
		t.Fatalf("valid login failed: %v", err)
	}
	if token == "" {
		t.Fatalf("valid login returned an empty token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if _, err := Login(s, "ana", "incorrecta"); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, expected ErrInvalidCredentials", err)
	}
}

func TestLoginNonexistentUser(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if _, err := Login(s, "nadie", "secreta1"); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, expected ErrInvalidCredentials", err)
	}
}

func TestValidateJWTValid(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	token, err := Login(s, "ana", "secreta1")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sub, err := ValidateJWT(token)
	if err != nil {
		t.Fatalf("validating a valid token failed: %v", err)
	}
	if sub != "ana" {
		t.Fatalf("username = %q, expected \"ana\"", sub)
	}
}

func TestValidateJWTInvalid(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	// Token with a different signature: well-formed, but signed with another secret.
	other := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "ana",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	signed, err := other.SignedString([]byte("secreto-equivocado"))
	if err != nil {
		t.Fatalf("could not sign the test token: %v", err)
	}

	cases := map[string]string{
		"made up":             "esto.no.es.jwt",
		"different signature": signed,
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateJWT(token); err != ErrInvalidToken {
				t.Fatalf("error = %v, expected ErrInvalidToken", err)
			}
		})
	}
}

func TestCapabilityTokenRoundTrip(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	blockIDs := []string{"blk-1", "blk-2", "blk-3"}

	token, err := GenerateCapabilityToken("ana", blockIDs, 30*time.Second)
	if err != nil {
		t.Fatalf("generating capability token failed: %v", err)
	}

	sub, got, err := ValidateCapabilityToken(token)
	if err != nil {
		t.Fatalf("validating capability token failed: %v", err)
	}
	if sub != "ana" {
		t.Fatalf("username = %q, expected \"ana\"", sub)
	}
	if len(got) != len(blockIDs) {
		t.Fatalf("got %d blocks, expected %d", len(got), len(blockIDs))
	}
	for i := range blockIDs {
		if got[i] != blockIDs[i] {
			t.Fatalf("block[%d] = %q, expected %q", i, got[i], blockIDs[i])
		}
	}
}

func TestTokensNoSonIntercambiables(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	// Un capability token no debe valer como token de sesión.
	capTok, err := GenerateCapabilityToken("ana", []string{"blk-1"}, 30*time.Second)
	if err != nil {
		t.Fatalf("generar capability token falló: %v", err)
	}
	if _, err := ValidateJWT(capTok); err != ErrInvalidToken {
		t.Fatalf("ValidateJWT aceptó un capability token: err = %v", err)
	}

	// Un token de sesión no debe valer como capability token.
	ses, err := generateSessionToken("ana", SessionTTL)
	if err != nil {
		t.Fatalf("generar token de sesión falló: %v", err)
	}
	if _, _, err := ValidateCapabilityToken(ses); err != ErrInvalidToken {
		t.Fatalf("ValidateCapabilityToken aceptó un token de sesión: err = %v", err)
	}
}
