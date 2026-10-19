package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// Aserción en tiempo de compilación: *AESGCM debe cumplir Cipher.
var _ Cipher = (*AESGCM)(nil)

// KeySize es el tamaño de clave esperado: 32 bytes para AES-256.
const KeySize = 32

// AESGCM cifra bloques con AES-GCM. El nonce se antepone al ciphertext.
type AESGCM struct {
	aead cipher.AEAD
}

// NewAESGCM crea el cifrador a partir de una clave de KeySize bytes.
func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes long, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &AESGCM{aead: aead}, nil
}

func (c *AESGCM) Encrypt(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal antepone el nonce y autentica el aad.
	return c.aead.Seal(nonce, nonce, plaintext, aad), nil
}

func (c *AESGCM) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext is too short")
	}

	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("decryption or authentication failed: %w", err)
	}

	return plaintext, nil
}
