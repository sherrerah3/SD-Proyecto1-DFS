package grpcserver

import (
	"context"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/datanode/replication"
)

// chunkSize es el tamaño de cada fragmento enviado en el streaming de GetBlock.
const chunkSize = 64 * 1024 // 64 KB

// Replicator reenvía un bloque a otros DataNodes. Lo satisface replication.Replicator.
type Replicator interface {
	Replicate(ctx context.Context, block replication.Block, targets []string) ([]string, error)
}

// Server implementa el servicio gRPC BlockService sobre un BlockStore.
type Server struct {
	datanodev1.UnimplementedBlockServiceServer

	store      blockstore.BlockStore
	replicator Replicator
}

// NewServer crea el servidor gRPC del DataNode sobre el store dado.
// replicator puede ser nil si este DataNode no debe reenviar réplicas.
func NewServer(store blockstore.BlockStore, replicator Replicator) *Server {
	return &Server{store: store, replicator: replicator}
}
