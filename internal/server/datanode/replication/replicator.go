package replication

import (
	"context"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// replicaChunkSize es el tamaño de cada fragmento al reenviar a una réplica.
const replicaChunkSize = 64 * 1024 // 64 KB

// Replicator reenvía bloques a otros DataNodes para cumplir la tasa de réplicas.
type Replicator struct {
	// dial permite inyectar la forma de conectarse (facilita las pruebas).
	dial          func(ctx context.Context, target string) (datanodev1.BlockServiceClient, func() error, error)
	clusterSecret string
}

// NewReplicator crea un Replicator que se conecta por gRPC a cada réplica.
func NewReplicator() *Replicator {
	return &Replicator{dial: dialGRPC}
}

// SetClusterSecret fija el secreto que se envía al destino.
func (r *Replicator) SetClusterSecret(secret string) {
	r.clusterSecret = secret
}

// Block describe el bloque a replicar y de dónde leer su contenido.
type Block struct {
	BlockID string
	SHA256  string
	Size    int64
	Open    func() (io.ReadCloser, error) // abre el contenido en claro del bloque
}

// Replicate reenvía el bloque a cada target. Las réplicas reciben replica_targets
// vacío para que no vuelvan a reenviar (corta el pipeline). Devuelve la lista de
// targets donde el bloque quedó almacenado con éxito.
func (r *Replicator) Replicate(ctx context.Context, block Block, targets []string) ([]string, error) {
	type result struct {
		target string
		err    error
	}

	results := make(chan result, len(targets))

	for _, target := range targets {
		go func(target string) {
			err := r.replicateOne(ctx, block, target)
			results <- result{target: target, err: err}
		}(target)
	}

	stored := make([]string, 0, len(targets))

	var firstErr error

	for i := 0; i < len(targets); i++ {
		res := <-results
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}

			continue
		}

		stored = append(stored, res.target)
	}

	return stored, firstErr
}

// replicateOne reenvía el bloque a un único target.
func (r *Replicator) replicateOne(ctx context.Context, block Block, target string) error {
	client, closeConn, err := r.dial(ctx, target)
	if err != nil {
		return err
	}

	defer closeConn()

	// Autenticación entre nodos.
	if r.clusterSecret != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-cluster-secret", r.clusterSecret)
	}

	stream, err := client.StoreBlock(ctx)
	if err != nil {
		return err
	}

	header := &datanodev1.StoreBlockRequest{
		Payload: &datanodev1.StoreBlockRequest_Header{
			Header: &datanodev1.StoreBlockHeader{
				BlockId:        block.BlockID,
				Sha256:         block.SHA256,
				SizeBytes:      block.Size,
				ReplicaTargets: nil, // vacío: la réplica no reenvía
			},
		},
	}

	if err := stream.Send(header); err != nil {
		return err
	}

	rc, err := block.Open()
	if err != nil {
		return err
	}

	defer rc.Close()

	buf := make([]byte, replicaChunkSize)

	for {
		n, readErr := rc.Read(buf)
		if n > 0 {
			chunk := &datanodev1.StoreBlockRequest{
				Payload: &datanodev1.StoreBlockRequest_Chunk{Chunk: buf[:n]},
			}

			if err := stream.Send(chunk); err != nil {
				return err
			}
		}

		if readErr == io.EOF {
			break
		}

		if readErr != nil {
			return readErr
		}
	}

	_, err = stream.CloseAndRecv()

	return err
}

// dialGRPC abre una conexión gRPC a un DataNode y devuelve el cliente y su cierre.
func dialGRPC(ctx context.Context, target string) (datanodev1.BlockServiceClient, func() error, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	return datanodev1.NewBlockServiceClient(conn), conn.Close, nil
}
