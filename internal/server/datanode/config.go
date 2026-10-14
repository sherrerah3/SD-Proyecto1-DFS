package datanode

import (
	"encoding/hex"
	"fmt"
	"os"

	"dfsha/internal/server/datanode/crypto"
)

const (
	envID   = "DFSHA_DATANODE_ID"
	envAddr = "DFSHA_DATANODE_ADDR"
	envDir  = "DFSHA_DATANODE_DIR"
	envKey  = "DFSHA_DATANODE_KEY"

	defaultAddr = ":9000"
	defaultDir  = "data/datanode"
)

// Config reúne los parámetros de arranque de un DataNode.
type Config struct {
	ID   string
	Addr string
	Dir  string
	Key  []byte // vacío = sin cifrado en reposo
}

// LoadConfig arma la configuración desde variables de entorno.
func LoadConfig() (Config, error) {
	cfg := Config{
		ID:   os.Getenv(envID),
		Addr: envOr(envAddr, defaultAddr),
		Dir:  envOr(envDir, defaultDir),
	}

	if cfg.ID == "" {
		return Config{}, fmt.Errorf("%s es obligatorio", envID)
	}

	rawKey := os.Getenv(envKey)
	if rawKey != "" {
		key, err := hex.DecodeString(rawKey)
		if err != nil {
			return Config{}, fmt.Errorf("%s debe ser hexadecimal: %w", envKey, err)
		}

		if len(key) != crypto.KeySize {
			return Config{}, fmt.Errorf("%s debe tener %d bytes (%d en hex)", envKey, crypto.KeySize, crypto.KeySize*2)
		}

		cfg.Key = key
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
