package heartbeat

import "context"

// Report es el estado que un DataNode comunica al Metanode en cada latido.
type Report struct {
	DataNodeID string
	Address    string
	FreeBytes  int64
	BlockCount int
}

// Reporter es el borde hacia el Metanode. La implementación real (gRPC) la
// provee el Metanode (Persona 2); aquí se depende solo de esta interfaz.
type Reporter interface {
	// Register anuncia el DataNode al Metanode al arrancar.
	Register(ctx context.Context, r Report) error

	// Beat envía un latido periódico con el estado actual.
	Beat(ctx context.Context, r Report) error
}
