package crypto

// Cipher define el cifrado simétrico de bloques en reposo.
type Cipher interface {
	// Encrypt cifra plaintext y devuelve el ciphertext autenticado.
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt revierte Encrypt. Falla si el ciphertext fue alterado.
	Decrypt(ciphertext []byte) ([]byte, error)
}
