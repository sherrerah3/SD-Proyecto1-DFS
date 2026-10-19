package grpcserver

import (
	"context"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
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

	store         blockstore.BlockStore
	replicator    Replicator
	validator     *auth.Validator
	clusterSecret string // llamadas entre nodos
	selfID        string
	reporter      BlockReporter
}

// NewServer crea el servidor gRPC del DataNode sobre el store dado.
// replicator puede ser nil si este DataNode no debe reenviar réplicas.
func NewServer(store blockstore.BlockStore, replicator Replicator) *Server {
	return &Server{store: store, replicator: replicator}
}

// WithAuth conecta el validador de tokens y el secreto de clúster.
func (s *Server) WithAuth(validator *auth.Validator, clusterSecret, selfID string) *Server {
	s.validator = validator
	s.clusterSecret = clusterSecret
	s.selfID = selfID
	return s
}

// BlockReporter avisa al Metanode de un bloque almacenado.
type BlockReporter interface {
	ReportBlock(ctx context.Context, blockID, dataNodeID, sha256 string) error
}

// WithReporter conecta el reporte de bloques al Metanode.
func (s *Server) WithReporter(r BlockReporter) *Server {
	s.reporter = r
	return s
}
