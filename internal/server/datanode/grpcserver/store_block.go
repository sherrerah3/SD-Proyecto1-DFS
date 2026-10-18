package grpcserver

import (
	"context"
	"fmt"
	"io"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/datanode/replication"
)

// StoreBlock recibe un bloque como stream (header + chunks) y lo persiste.
func (s *Server) StoreBlock(stream datanodev1.BlockService_StoreBlockServer) error {
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "no se recibió el header: %v", err)
	}

	header := first.GetHeader()
	if header == nil {
		return status.Error(codes.InvalidArgument, "el primer mensaje debe ser el header")
	}

	if header.GetBlockId() == "" {
		return status.Error(codes.InvalidArgument, "block_id vacío")
	}

	// Réplica (secreto de clúster) o cliente (capability op=put).
	if s.requireClusterSecret(stream.Context()) != nil {
		cap, err := s.capabilityFromContext(stream.Context(), auth.OpPut)
		if err != nil {
			return err
		}
		if err := s.authorizeBlock(cap, header.GetBlockId()); err != nil {
			return err
		}
	}

	// Un pipe conecta la recepción del stream con la escritura al store:
	// una goroutine vuelca los chunks al pipe mientras Write lee de él.
	pr, pw := io.Pipe()

	go func() {
		pw.CloseWithError(drainChunks(stream, pw))
	}()

	info, err := s.store.Write(header.GetBlockId(), pr)
	if err != nil {
		pr.CloseWithError(err)
		return status.Errorf(codes.Internal, "falló al almacenar el bloque: %v", err)
	}

	// Si el header declara un hash, se valida contra el calculado al escribir.
	// Si no coincide, se borra el bloque corrupto.
	if header.GetSha256() != "" && header.GetSha256() != info.SHA256 {
		_, _ = s.store.Delete(info.BlockID)
		return status.Errorf(
			codes.DataLoss,
			"hash no coincide: esperado %s, calculado %s",
			header.GetSha256(),
			info.SHA256,
		)
	}

	// Reporte al Metanode para el quórum.
	if s.reporter != nil {
		if err := s.reporter.ReportBlock(stream.Context(), info.BlockID, s.selfID, info.SHA256); err != nil {
			log.Printf("no se pudo reportar el bloque %s al metanode: %v", info.BlockID, err)
		}
	}

	// El bloque ya quedó en este DataNode. Si hay réplicas por cubrir, se
	// reenvía a cada una; los fallos no abortan el put (la re-replicación
	// posterior la dispara el Metanode), solo se reflejan en stored_on.
	storedOn := replicateBlock(stream.Context(), s, header, info)

	return stream.SendAndClose(&datanodev1.StoreBlockResponse{
		BlockId:   info.BlockID,
		Sha256:    info.SHA256,
		SizeBytes: info.SizeBytes,
		StoredOn:  storedOn,
	})
}

// replicateBlock reenvía el bloque a los replica_targets del header, si los hay.
// Devuelve la lista de DataNodes donde el bloque quedó almacenado (este incluido).
func replicateBlock(
	ctx context.Context,
	s *Server,
	header *datanodev1.StoreBlockHeader,
	info blockstore.BlockInfo,
) []string {
	stored := []string{"self"}

	targets := header.GetReplicaTargets()
	if len(targets) == 0 || s.replicator == nil {
		return stored
	}

	block := replication.Block{
		BlockID: info.BlockID,
		SHA256:  info.SHA256,
		Size:    info.SizeBytes,
		Open: func() (io.ReadCloser, error) {
			rc, _, err := s.store.Read(info.BlockID)
			return rc, err
		},
	}

	replicas, err := s.replicator.Replicate(ctx, block, targets)
	if err != nil {
		log.Printf("replicación parcial del bloque %s: %v", info.BlockID, err)
	}

	return append(stored, replicas...)
}

// drainChunks vuelca los chunks restantes del stream al writer dado.
func drainChunks(stream datanodev1.BlockService_StoreBlockServer, w io.Writer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}

		if err != nil {
			return err
		}

		chunk := req.GetChunk()
		if chunk == nil {
			return fmt.Errorf("se esperaba un chunk, llegó otro tipo de mensaje")
		}

		if _, err := w.Write(chunk); err != nil {
			return err
		}
	}
}
