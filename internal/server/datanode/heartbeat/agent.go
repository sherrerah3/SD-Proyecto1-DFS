package heartbeat

import (
	"context"
	"log"
	"time"

	"dfsha/internal/server/datanode/blockstore"
)

// DefaultInterval es el periodo de latido definido en el diseño (5 s).
const DefaultInterval = 5 * time.Second

// Agent reporta periódicamente el estado del DataNode al Metanode.
type Agent struct {
	datanodeID string
	address    string
	store      blockstore.BlockStore
	reporter   Reporter
	freeBytes  func() int64
	interval   time.Duration
}

// NewAgent crea el agente de heartbeat. freeBytes consulta el espacio libre
// del almacenamiento; si es nil, se reporta 0.
func NewAgent(
	datanodeID string,
	address string,
	store blockstore.BlockStore,
	reporter Reporter,
	freeBytes func() int64,
) *Agent {
	return &Agent{
		datanodeID: datanodeID,
		address:    address,
		store:      store,
		reporter:   reporter,
		freeBytes:  freeBytes,
		interval:   DefaultInterval,
	}
}

// SetInterval ajusta el periodo entre latidos.
func (a *Agent) SetInterval(d time.Duration) {
	a.interval = d
}

// Run registra el DataNode y envía latidos hasta que ctx se cancele.
func (a *Agent) Run(ctx context.Context) error {
	if err := a.reporter.Register(ctx, a.snapshot()); err != nil {
		return err
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			if err := a.reporter.Beat(ctx, a.snapshot()); err != nil {
				// Un latido fallido no detiene el agente: se reintenta al siguiente.
				log.Printf("heartbeat del datanode %s falló: %v", a.datanodeID, err)
			}
		}
	}
}

// snapshot arma el reporte con el estado actual del DataNode.
func (a *Agent) snapshot() Report {
	return Report{
		DataNodeID: a.datanodeID,
		Address:    a.address,
		FreeBytes:  a.currentFreeBytes(),
		BlockCount: a.currentBlockCount(),
	}
}

func (a *Agent) currentFreeBytes() int64 {
	if a.freeBytes == nil {
		return 0
	}

	return a.freeBytes()
}

func (a *Agent) currentBlockCount() int {
	reports, err := a.store.List()
	if err != nil {
		log.Printf("no se pudo contar los bloques del datanode %s: %v", a.datanodeID, err)
		return 0
	}

	return len(reports)
}
