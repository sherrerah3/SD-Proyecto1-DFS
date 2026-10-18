package heartbeat

import "context"

// Report es el estado que un DataNode comunica al Metanode en cada latido.
type Report struct {
	DataNodeID string
	Address    string
	FreeBytes  int64
	BlockCount int
}

// Reporter es el borde hacia el Metanode.
type Reporter interface {
	// Register anuncia el DataNode al Metanode al arrancar.
	Register(ctx context.Context, r Report) error

	// Beat envía un latido y devuelve los bloques a borrar.
	Beat(ctx context.Context, r Report) ([]string, error)
}

// BlockDeleter aplica los borrados ordenados por el Metanode.
type BlockDeleter interface {
	Delete(blockID string) (bool, error)
}
