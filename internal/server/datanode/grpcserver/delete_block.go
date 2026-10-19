package grpcserver

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// DeleteBlock elimina un bloque del disco.
func (s *Server) DeleteBlock(ctx context.Context, req *datanodev1.DeleteBlockRequest) (*datanodev1.DeleteBlockResponse, error) {
	if req.GetBlockId() == "" {
		return nil, status.Error(codes.InvalidArgument, "empty block_id")
	}

	// Solo nodos del clúster pueden borrar.
	if err := s.requireClusterSecret(ctx); err != nil {
		return nil, err
	}

	deleted, err := s.store.Delete(req.GetBlockId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete block: %v", err)
	}
	log.Printf("datanode %s: block deletion processed block_id=%s deleted=%t remote=%s", s.selfID, req.GetBlockId(), deleted, remoteAddress(ctx))

	return &datanodev1.DeleteBlockResponse{
		BlockId: req.GetBlockId(),
		Deleted: deleted,
	}, nil
}
