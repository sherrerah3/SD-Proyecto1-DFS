package grpcserver

import (
	"io"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
)

// GetBlock devuelve un bloque como stream: primero el header, luego los chunks.
func (s *Server) GetBlock(req *datanodev1.GetBlockRequest, stream datanodev1.BlockService_GetBlockServer) error {
	if req.GetBlockId() == "" {
		return status.Error(codes.InvalidArgument, "empty block_id")
	}

	// Exige capability op=get que incluya el bloque.
	cap, err := s.capabilityFromContext(stream.Context(), auth.OpGet)
	if err != nil {
		return err
	}
	if err := s.authorizeBlock(cap, req.GetBlockId()); err != nil {
		return err
	}

	log.Printf("datanode %s: block download requested block_id=%s remote=%s", s.selfID, req.GetBlockId(), remoteAddress(stream.Context()))
	rc, info, err := s.store.Read(req.GetBlockId())
	if err != nil {
		return status.Errorf(codes.NotFound, "block not found: %v", err)
	}

	defer rc.Close()

	header := &datanodev1.GetBlockResponse{
		Payload: &datanodev1.GetBlockResponse_Header{
			Header: &datanodev1.GetBlockHeader{
				BlockId:   info.BlockID,
				Sha256:    info.SHA256,
				SizeBytes: info.SizeBytes,
			},
		},
	}

	if err := stream.Send(header); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)

	for {
		n, readErr := rc.Read(buf)
		if n > 0 {
			chunk := &datanodev1.GetBlockResponse{
				Payload: &datanodev1.GetBlockResponse_Chunk{Chunk: buf[:n]},
			}

			if err := stream.Send(chunk); err != nil {
				return err
			}
		}

		if readErr == io.EOF {
			log.Printf("datanode %s: block download completed block_id=%s size_bytes=%d remote=%s", s.selfID, info.BlockID, info.SizeBytes, remoteAddress(stream.Context()))
			return nil
		}

		if readErr != nil {
			return status.Errorf(codes.Internal, "failed to read block: %v", readErr)
		}
	}
}
