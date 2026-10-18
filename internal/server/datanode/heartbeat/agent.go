package heartbeat

import (
	"context"
	"errors"
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
	deleter    BlockDeleter // aplica las órdenes de borrado del Metanode
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

// SetDeleter conecta el store local para aplicar las órdenes de borrado.
func (a *Agent) SetDeleter(d BlockDeleter) {
	a.deleter = d
}

// SetInterval ajusta el periodo entre latidos.
func (a *Agent) SetInterval(d time.Duration) {
	a.interval = d
}

// Run registra el DataNode y envía latidos hasta que ctx se cancele.
func (a *Agent) Run(ctx context.Context) error {
	if err := a.registerWithRetry(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			toDelete, err := a.reporter.Beat(ctx, a.snapshot())
			if errors.Is(err, ErrNotRegistered) {
				// El Metanode no nos conoce (p. ej. tras failover).
				log.Printf("datanode %s no registrado; re-registrando", a.datanodeID)
				if rerr := a.reporter.Register(ctx, a.snapshot()); rerr != nil {
					log.Printf("re-registro del datanode %s falló: %v", a.datanodeID, rerr)
				}
				continue
			}
			if err != nil {
				// Un latido fallido no detiene el agente: se reintenta al siguiente.
				log.Printf("heartbeat del datanode %s falló: %v", a.datanodeID, err)
				continue
			}
			a.applyDeletions(toDelete)
		}
	}
}

// registerWithRetry reintenta el registro con espera creciente.
func (a *Agent) registerWithRetry(ctx context.Context) error {
	backoff := 1 * time.Second
	const maxBackoff = 15 * time.Second
	for {
		if err := a.reporter.Register(ctx, a.snapshot()); err == nil {
			return nil
		} else {
			log.Printf("registro del datanode %s falló: %v (reintentando en %s)", a.datanodeID, err, backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// applyDeletions borra localmente los bloques que ordenó el Metanode.
func (a *Agent) applyDeletions(blockIDs []string) {
	if a.deleter == nil {
		return
	}
	for _, id := range blockIDs {
		if _, err := a.deleter.Delete(id); err != nil {
			log.Printf("datanode %s no pudo borrar el bloque %s: %v", a.datanodeID, id, err)
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

// currentBlockCount no recorre el disco; el Metanode no usa el dato.
func (a *Agent) currentBlockCount() int {
	return 0
}
