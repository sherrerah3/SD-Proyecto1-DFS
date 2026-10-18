package crypto

import (
	"bytes"
	"testing"
)

func newTestCipher(t *testing.T) *AESGCM {
	t.Helper()

	c, err := NewAESGCM(bytes.Repeat([]byte{0x01}, KeySize))
	if err != nil {
		t.Fatalf("NewAESGCM() error inesperado: %v", err)
	}

	return c
}

func TestNewAESGCMKeySize(t *testing.T) {
	tests := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{name: "clave de 32 bytes", keyLen: KeySize, wantErr: false},
		{name: "clave muy corta", keyLen: 16, wantErr: true},
		{name: "clave vacía", keyLen: 0, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAESGCM(bytes.Repeat([]byte{0x01}, tc.keyLen))
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewAESGCM() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)

	plaintext := []byte("contenido secreto del bloque")
	aad := []byte("blk-1")

	ciphertext, err := c.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt() error inesperado: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatalf("el ciphertext no debería igualar al plaintext")
	}

	got, err := c.Decrypt(ciphertext, aad)
	if err != nil {
		t.Fatalf("Decrypt() error inesperado: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt() devolvió %q, se esperaba %q", got, plaintext)
	}

	// Otro AAD debe fallar
	if _, err := c.Decrypt(ciphertext, []byte("blk-2")); err == nil {
		t.Fatalf("Decrypt() debió fallar con un AAD distinto")
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	c := newTestCipher(t)

	plaintext := []byte("mismo contenido")

	a, err := c.Encrypt(plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt() error inesperado: %v", err)
	}

	b, err := c.Encrypt(plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt() error inesperado: %v", err)
	}

	// Con nonce aleatorio, dos cifrados del mismo claro no deben coincidir.
	if bytes.Equal(a, b) {
		t.Fatalf("dos cifrados del mismo plaintext no deberían ser iguales")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	c := newTestCipher(t)

	ciphertext, err := c.Encrypt([]byte("datos íntegros"), nil)
	if err != nil {
		t.Fatalf("Encrypt() error inesperado: %v", err)
	}

	// Se altera el último byte: GCM debe rechazar la autenticación.
	ciphertext[len(ciphertext)-1] ^= 0xFF

	if _, err := c.Decrypt(ciphertext, nil); err == nil {
		t.Fatalf("Decrypt() debió fallar ante un ciphertext alterado")
	}
}

func TestDecryptRejectsShortInput(t *testing.T) {
	c := newTestCipher(t)

	if _, err := c.Decrypt([]byte{0x00, 0x01}, nil); err == nil {
		t.Fatalf("Decrypt() debió fallar ante un ciphertext demasiado corto")
	}
}
