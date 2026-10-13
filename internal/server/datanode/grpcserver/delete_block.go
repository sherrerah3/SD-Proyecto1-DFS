package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// DeleteBlock elimina un bloque del disco.
func (s *Server) DeleteBlock(ctx context.Context, req *datanodev1.DeleteBlockRequest) (*datanodev1.DeleteBlockResponse, error) {
	if req.GetBlockId() == "" {
		return nil, status.Error(codes.InvalidArgument, "block_id vacío")
	}

	deleted, err := s.store.Delete(req.GetBlockId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "falló al borrar el bloque: %v", err)
	}

	return &datanodev1.DeleteBlockResponse{
		BlockId: req.GetBlockId(),
		Deleted: deleted,
	}, nil
}
