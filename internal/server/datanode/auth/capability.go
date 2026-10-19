// Package auth valida los capability tokens del Metanode.
package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// Operaciones autorizables (deben coincidir con las del Metanode).
const (
	OpPut = "put"
	OpGet = "get"
)

var ErrInvalidCapability = errors.New("invalid or expired capability token")

// Capability es el contenido útil de un token ya validado.
type Capability struct {
	Username string
	Op       string
	BlockIDs []string
}

// Validator verifica capability tokens firmados con la clave compartida.
type Validator struct {
	secret []byte
}

// NewValidator crea un validador con la clave de firma de capacidades.
func NewValidator(secret []byte) *Validator {
	return &Validator{secret: secret}
}

// Validate verifica firma, expiración, tipo y operación (requireOp)
func (v *Validator) Validate(tokenString, requireOp string) (Capability, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidCapability
		}
		return v.secret, nil
	})
	if err != nil || !token.Valid {
		return Capability{}, ErrInvalidCapability
	}

	if typ, _ := claims["typ"].(string); typ != "capability" {
		return Capability{}, ErrInvalidCapability
	}

	op, _ := claims["op"].(string)
	if op != requireOp {
		return Capability{}, ErrInvalidCapability
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return Capability{}, ErrInvalidCapability
	}

	rawBlocks, ok := claims["block_ids"].([]any)
	if !ok {
		return Capability{}, ErrInvalidCapability
	}
	blockIDs := make([]string, 0, len(rawBlocks))
	for _, item := range rawBlocks {
		id, ok := item.(string)
		if !ok {
			return Capability{}, ErrInvalidCapability
		}
		blockIDs = append(blockIDs, id)
	}

	return Capability{Username: sub, Op: op, BlockIDs: blockIDs}, nil
}

// Allows indica si la capability autoriza el block_id dado.
func (c Capability) Allows(blockID string) bool {
	for _, id := range c.BlockIDs {
		if id == blockID {
			return true
		}
	}
	return false
}
