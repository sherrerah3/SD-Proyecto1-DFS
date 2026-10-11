package service

import (
	"testing"
	"time"

	"dfsha/internal/server/metanode/store"
)

const testSecret = "test-secret-para-pruebas"

func TestRegister(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()

	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("registro válido falló: %v", err)
	}
	if err := Register(s, "ana", "secreta1"); err != ErrUserAlreadyExists {
		t.Fatalf("duplicado: error = %v, se esperaba ErrUserAlreadyExists", err)
	}
	if err := Register(s, "", "secreta1"); err != ErrInvalidUsername {
		t.Fatalf("username vacío: error = %v, se esperaba ErrInvalidUsername", err)
	}
	if err := Register(s, "bob", "123"); err != ErrInvalidPassword {
		t.Fatalf("password corta: error = %v, se esperaba ErrInvalidPassword", err)
	}
}

func TestLogin(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	s := store.NewMemoryStore()
	if err := Register(s, "ana", "secreta1"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	token, err := Login(s, "ana", "secreta1")
	if err != nil || token == "" {
		t.Fatalf("login válido falló: token=%q err=%v", token, err)
	}
	if _, err := Login(s, "ana", "incorrecta"); err != ErrInvalidCredentials {
		t.Fatalf("password mala: error = %v, se esperaba ErrInvalidCredentials", err)
	}
	if _, err := Login(s, "nadie", "secreta1"); err != ErrInvalidCredentials {
		t.Fatalf("usuario inexistente: error = %v, se esperaba ErrInvalidCredentials", err)
	}
}

func TestValidateJWT(t *testing.T) {
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
	if err != nil || sub != "ana" {
		t.Fatalf("token válido: sub=%q err=%v", sub, err)
	}
	if _, err := ValidateJWT("token.invalido"); err == nil {
		t.Fatalf("token basura: se esperaba error")
	}
}

func TestCapabilityToken(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, testSecret)
	blockIDs := []string{"blk-1", "blk-2"}

	token, err := GenerateCapabilityToken("ana", blockIDs, 30*time.Second)
	if err != nil {
		t.Fatalf("generar capability token falló: %v", err)
	}

	sub, got, err := ValidateCapabilityToken(token)
	if err != nil || sub != "ana" || len(got) != len(blockIDs) {
		t.Fatalf("validar capability token: sub=%q bloques=%v err=%v", sub, got, err)
	}
}

func TestMissingJWTSecret(t *testing.T) {
	t.Setenv(jwtSecretEnvVar, "")

	if _, err := generateSessionToken("ana", time.Minute); err != ErrMissingJWTSecret {
		t.Fatalf("sin secreto: error = %v, se esperaba ErrMissingJWTSecret", err)
	}
}
