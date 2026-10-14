package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// VerifyBlock recalcula el hash de un bloque desde disco.
func (s *Server) VerifyBlock(ctx context.Context, req *datanodev1.VerifyBlockRequest) (*datanodev1.VerifyBlockResponse, error) {
	if req.GetBlockId() == "" {
		return nil, status.Error(codes.InvalidArgument, "block_id vacío")
	}

	report, exists, err := s.store.Verify(req.GetBlockId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "falló al verificar el bloque: %v", err)
	}

	return &datanodev1.VerifyBlockResponse{
		BlockId: req.GetBlockId(),
		Exists:  exists,
		Sha256:  report.SHA256,
	}, nil
}
