package grpcserver

import (
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// GetBlock devuelve un bloque como stream: primero el header, luego los chunks.
func (s *Server) GetBlock(req *datanodev1.GetBlockRequest, stream datanodev1.BlockService_GetBlockServer) error {
	if req.GetBlockId() == "" {
		return status.Error(codes.InvalidArgument, "block_id vacío")
	}

	rc, info, err := s.store.Read(req.GetBlockId())
	if err != nil {
		return status.Errorf(codes.NotFound, "bloque no encontrado: %v", err)
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
			return nil
		}

		if readErr != nil {
			return status.Errorf(codes.Internal, "falló la lectura del bloque: %v", readErr)
		}
	}
}
