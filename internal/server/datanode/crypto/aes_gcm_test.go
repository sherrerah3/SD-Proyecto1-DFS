package crypto

import (
	"bytes"
	"testing"
)

func newTestCipher(t *testing.T) *AESGCM {
	t.Helper()

	c, err := NewAESGCM(bytes.Repeat([]byte{0x01}, KeySize))
	if err != nil {
		t.Fatalf("NewAESGCM() error unexpected: %v", err)
	}

	return c
}

func TestNewAESGCMKeySize(t *testing.T) {
	tests := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{name: "32-byte key", keyLen: KeySize, wantErr: false},
		{name: "key too short", keyLen: 16, wantErr: true},
		{name: "empty key", keyLen: 0, wantErr: true},
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

	plaintext := []byte("secret block content")
	aad := []byte("blk-1")

	ciphertext, err := c.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt() error unexpected: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatalf("ciphertext should not match plaintext")
	}

	got, err := c.Decrypt(ciphertext, aad)
	if err != nil {
		t.Fatalf("Decrypt() error unexpected: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt() returned %q, expected %q", got, plaintext)
	}

	// Otro AAD debe fallar
	if _, err := c.Decrypt(ciphertext, []byte("blk-2")); err == nil {
		t.Fatalf("Decrypt() should fail with a different AAD")
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	c := newTestCipher(t)

	plaintext := []byte("same content")

	a, err := c.Encrypt(plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt() error unexpected: %v", err)
	}

	b, err := c.Encrypt(plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt() error unexpected: %v", err)
	}

	// Con nonce aleatorio, dos cifrados del mismo claro no deben coincidir.
	if bytes.Equal(a, b) {
		t.Fatalf("two encryptions of the same plaintext should not be equal")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	c := newTestCipher(t)

	ciphertext, err := c.Encrypt([]byte("datos íntegros"), nil)
	if err != nil {
		t.Fatalf("Encrypt() error unexpected: %v", err)
	}

	// Se altera el último byte: GCM debe rechazar la autenticación.
	ciphertext[len(ciphertext)-1] ^= 0xFF

	if _, err := c.Decrypt(ciphertext, nil); err == nil {
		t.Fatalf("Decrypt() should fail with a modified ciphertext")
	}
}

func TestDecryptRejectsShortInput(t *testing.T) {
	c := newTestCipher(t)

	if _, err := c.Decrypt([]byte{0x00, 0x01}, nil); err == nil {
		t.Fatalf("Decrypt() should fail when the ciphertext is too short")
	}
}
