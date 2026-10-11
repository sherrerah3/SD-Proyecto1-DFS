package service

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"dfsha/internal/server/metanode/store"
)

const testSecret = "test-secret-para-pruebas"

func TestRegisterValido(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("registro válido falló: %v", err)
	}

	u, err := s.GetUser("ana")
	if err != nil {
		t.Fatalf("el usuario no quedó en el store: %v", err)
	}
	if u.PasswordHash == "" || u.PasswordHash == "secreta1" {
		t.Fatalf("la contraseña no quedó encriptada: %q", u.PasswordHash)
	}
}

func TestRegisterDuplicado(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("primer registro falló: %v", err)
	}
	if err := Register(s, "ana", "secreta1"); err != ErrUserAlreadyExists {
		t.Fatalf("segundo registro: error = %v, se esperaba ErrUserAlreadyExists", err)
	}
}

func TestRegisterUsernameInvalido(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	casos := map[string]string{
		"vacío":     "",
		"muy largo": strings.Repeat("a", 17),
	}

	for nombre, username := range casos {
		t.Run(nombre, func(t *testing.T) {
			s := store.NewMemoryStore()
			if err := Register(s, username, "secreta1"); err != ErrInvalidUsername {
				t.Fatalf("error = %v, se esperaba ErrInvalidUsername", err)
			}
		})
	}
}

func TestRegisterPasswordInvalida(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	casos := map[string]string{
		"muy corta": "abc",
		"muy larga": strings.Repeat("a", 73),
	}

	for nombre, password := range casos {
		t.Run(nombre, func(t *testing.T) {
			s := store.NewMemoryStore()
			if err := Register(s, "ana", password); err != ErrInvalidPassword {
				t.Fatalf("error = %v, se esperaba ErrInvalidPassword", err)
			}
		})
	}
}

func TestLoginValido(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	token, err := Login(s, "ana", "secreta1")
	if err != nil {
		t.Fatalf("login válido falló: %v", err)
	}
	if token == "" {
		t.Fatalf("login válido devolvió token vacío")
	}
}

func TestLoginPasswordIncorrecta(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	if _, err := Login(s, "ana", "incorrecta"); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, se esperaba ErrInvalidCredentials", err)
	}
}

func TestLoginUsuarioInexistente(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if _, err := Login(s, "nadie", "secreta1"); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, se esperaba ErrInvalidCredentials", err)
	}
}

func TestValidateJWTValido(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	token, err := Login(s, "ana", "secreta1")
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	sub, err := ValidateJWT(token)
	if err != nil {
		t.Fatalf("validar token válido falló: %v", err)
	}
	if sub != "ana" {
		t.Fatalf("username = %q, se esperaba \"ana\"", sub)
	}
}

func TestValidateJWTInvalido(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)

	// Token con otra firma: válido en forma, pero firmado con un secreto distinto.
	otro := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "ana",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	firmado, err := otro.SignedString([]byte("secreto-equivocado"))
	if err != nil {
		t.Fatalf("no se pudo firmar el token de prueba: %v", err)
	}

	casos := map[string]string{
		"inventado":      "esto.no.es.jwt",
		"firma distinta": firmado,
	}

	for nombre, token := range casos {
		t.Run(nombre, func(t *testing.T) {
			if _, err := ValidateJWT(token); err != ErrInvalidToken {
				t.Fatalf("error = %v, se esperaba ErrInvalidToken", err)
			}
		})
	}
}

func TestCapabilityTokenIdaYVuelta(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	blockIDs := []string{"blk-1", "blk-2", "blk-3"}

	token, err := GenerateCapabilityToken("ana", blockIDs, 30*time.Second)
	if err != nil {
		t.Fatalf("generar capability token falló: %v", err)
	}

	sub, got, err := ValidateCapabilityToken(token)
	if err != nil {
		t.Fatalf("validar capability token falló: %v", err)
	}
	if sub != "ana" {
		t.Fatalf("username = %q, se esperaba \"ana\"", sub)
	}
	if len(got) != len(blockIDs) {
		t.Fatalf("se devolvieron %d bloques, se esperaban %d", len(got), len(blockIDs))
	}
	for i := range blockIDs {
		if got[i] != blockIDs[i] {
			t.Fatalf("bloque[%d] = %q, se esperaba %q", i, got[i], blockIDs[i])
		}
	}
}
