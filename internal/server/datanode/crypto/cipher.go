package crypto

// Cipher define el cifrado simétrico de bloques en reposo.
type Cipher interface {
	// Encrypt cifra plaintext; aad liga el cifrado a un contexto (block_id).
	Encrypt(plaintext, aad []byte) ([]byte, error)

	// Decrypt revierte Encrypt. Falla si el ciphertext o el aad no coinciden.
	Decrypt(ciphertext, aad []byte) ([]byte, error)
}
