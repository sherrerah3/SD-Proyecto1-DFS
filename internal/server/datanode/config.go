package datanode

import (
	"encoding/hex"
	"fmt"
	"os"

	"dfsha/internal/server/datanode/crypto"
)

const (
	envID             = "DFSHA_DATANODE_ID"
	envAddr           = "DFSHA_DATANODE_ADDR"
	envDir            = "DFSHA_DATANODE_DIR"
	envKey            = "DFSHA_DATANODE_KEY"
	envAllowPlaintext = "DFSHA_DATANODE_ALLOW_PLAINTEXT"
	envCapability     = "DFSHA_CAPABILITY_SECRET"
	envCluster        = "DFSHA_CLUSTER_SECRET"

	defaultAddr = ":9000"
	defaultDir  = "data/datanode"
)

// Config reúne los parámetros de arranque de un DataNode.
type Config struct {
	ID               string
	Addr             string
	Dir              string
	Key              []byte // clave AES-256 de cifrado en reposo
	CapabilitySecret []byte // valida los capability tokens del Metanode
	ClusterSecret    string // autentica llamadas entre nodos (borrado, replicación)
}

// LoadConfig arma la configuración desde variables de entorno.
func LoadConfig() (Config, error) {
	cfg := Config{
		ID:            os.Getenv(envID),
		Addr:          envOr(envAddr, defaultAddr),
		Dir:           envOr(envDir, defaultDir),
		ClusterSecret: os.Getenv(envCluster),
	}

	if cfg.ID == "" {
		return Config{}, fmt.Errorf("%s is required", envID)
	}

	if cfg.ClusterSecret == "" {
		return Config{}, fmt.Errorf("%s is required", envCluster)
	}

	if secret := os.Getenv(envCapability); secret != "" {
		cfg.CapabilitySecret = []byte(secret)
	} else {
		return Config{}, fmt.Errorf("%s is required", envCapability)
	}

	rawKey := os.Getenv(envKey)
	if rawKey != "" {
		key, err := hex.DecodeString(rawKey)
		if err != nil {
			return Config{}, fmt.Errorf("%s must be hexadecimal: %w", envKey, err)
		}

		if len(key) != crypto.KeySize {
			return Config{}, fmt.Errorf("%s must be %d bytes (%d in hex)", envKey, crypto.KeySize, crypto.KeySize*2)
		}

		cfg.Key = key
	} else if os.Getenv(envAllowPlaintext) != "1" {
		// Cifrado obligatorio salvo opt-in explícito (solo desarrollo).
		return Config{}, fmt.Errorf(
			"%s is required; to store data unencrypted (development only), set %s=1",
			envKey, envAllowPlaintext,
		)
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
