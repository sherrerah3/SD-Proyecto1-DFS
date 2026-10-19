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

	// Synchronize local disk contents as soon as registration succeeds rather
	// than waiting a full heartbeat interval after a restart.
	a.sendHeartbeat(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

// registerWithRetry reintenta el registro con espera creciente.
func (a *Agent) registerWithRetry(ctx context.Context) error {
	backoff := 1 * time.Second
	const maxBackoff = 15 * time.Second
	for {
		if err := a.reporter.Register(ctx, a.snapshot()); err == nil {
			log.Printf("datanode %s: registered with MetaNode at %s", a.datanodeID, a.address)
			return nil
		} else {
			log.Printf("registration of datanode %s failed: %v (retrying in %s)", a.datanodeID, err, backoff)
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
		deleted, err := a.deleter.Delete(id)
		if err != nil {
			log.Printf("datanode %s failed to delete block %s: %v", a.datanodeID, id, err)
			continue
		}
		log.Printf("datanode %s: processed MetaNode block-release command block_id=%s file_removed=%t storage_available=true", a.datanodeID, id, deleted)
	}
}

// sendHeartbeat reports the current disk inventory and applies deletion commands.
func (a *Agent) sendHeartbeat(ctx context.Context) {
	report := a.snapshot()
	toDelete, err := a.reporter.Beat(ctx, report)
	if errors.Is(err, ErrNotRegistered) {
		log.Printf("datanode %s is not registered; registering again", a.datanodeID)
		if rerr := a.reporter.Register(ctx, a.snapshot()); rerr != nil {
			log.Printf("re-registration of datanode %s failed: %v", a.datanodeID, rerr)
		} else {
			log.Printf("datanode %s: re-registered with MetaNode at %s", a.datanodeID, a.address)
		}
		return
	}
	if err != nil {
		log.Printf("heartbeat from datanode %s failed: %v", a.datanodeID, err)
		return
	}
	log.Printf("datanode %s: heartbeat acknowledged by MetaNode reported_blocks=%d pending_deletions=%d", a.datanodeID, report.BlockCount, len(toDelete))
	a.applyDeletions(toDelete)
}

// snapshot builds a report using a fresh inventory of the local disk.
func (a *Agent) snapshot() Report {
	blockIDs, err := a.store.ListIDs()
	if err != nil {
		log.Printf("datanode %s: failed to list local block inventory: %v", a.datanodeID, err)
		blockIDs = nil // nil means that this report's inventory is unavailable.
	}
	blocks := make([]blockstore.BlockReport, 0, len(blockIDs))
	if blockIDs != nil {
		for _, id := range blockIDs {
			blocks = append(blocks, blockstore.BlockReport{BlockID: id})
		}
	}
	return Report{
		DataNodeID: a.datanodeID,
		Address:    a.address,
		FreeBytes:  a.currentFreeBytes(),
		BlockCount: len(blocks),
		Blocks:     blocks,
	}
}

func (a *Agent) currentFreeBytes() int64 {
	if a.freeBytes == nil {
		return 0
	}

	return a.freeBytes()
}
