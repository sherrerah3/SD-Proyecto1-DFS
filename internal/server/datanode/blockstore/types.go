package blockstore

// BlockInfo describe un bloque almacenado en disco.
type BlockInfo struct {
	BlockID   string
	SHA256    string
	SizeBytes int64
}

// BlockReport es el estado de integridad de un bloque, recalculado desde disco.
type BlockReport struct {
	BlockID string
	SHA256  string
}
