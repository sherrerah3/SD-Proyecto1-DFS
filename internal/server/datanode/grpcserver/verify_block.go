package grpcserver

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// VerifyBlock recalcula el hash de un bloque desde disco.
func (s *Server) VerifyBlock(ctx context.Context, req *datanodev1.VerifyBlockRequest) (*datanodev1.VerifyBlockResponse, error) {
	if req.GetBlockId() == "" {
		return nil, status.Error(codes.InvalidArgument, "empty block_id")
	}

	report, exists, err := s.store.Verify(req.GetBlockId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to verify block: %v", err)
	}

	log.Printf("datanode %s: block verification completed block_id=%s exists=%t sha256=%s remote=%s", s.selfID, req.GetBlockId(), exists, report.SHA256, remoteAddress(ctx))
	return &datanodev1.VerifyBlockResponse{
		BlockId: req.GetBlockId(),
		Exists:  exists,
		Sha256:  report.SHA256,
	}, nil
}
