package replication

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	datanodev1 "dfsha/internal/proto/datanode/v1"
)

// fakeReplica es un BlockService mínimo que guarda en memoria lo que recibe por
// StoreBlock. Se usa como destino de replicación sin depender de grpcserver,
// evitando un ciclo de importación.
type fakeReplica struct {
	datanodev1.UnimplementedBlockServiceServer

	lis *bufconn.Listener

	mu     sync.Mutex
	blocks map[string][]byte
}

func newFakeReplica(t *testing.T) *fakeReplica {
	t.Helper()

	f := &fakeReplica{
		lis:    bufconn.Listen(1024 * 1024),
		blocks: make(map[string][]byte),
	}

	srv := grpc.NewServer()
	datanodev1.RegisterBlockServiceServer(srv, f)

	go func() {
		_ = srv.Serve(f.lis)
	}()

	t.Cleanup(srv.Stop)

	return f
}

func (f *fakeReplica) StoreBlock(stream datanodev1.BlockService_StoreBlockServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}

	header := first.GetHeader()
	if header == nil {
		return errors.New("se esperaba el header primero")
	}

	// Las réplicas no deben recibir más targets (el pipeline no debe propagarse).
	if len(header.GetReplicaTargets()) != 0 {
		return errors.New("una réplica no debería recibir replica_targets")
	}

	var buf bytes.Buffer

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		buf.Write(req.GetChunk())
	}

	f.mu.Lock()
	f.blocks[header.GetBlockId()] = buf.Bytes()
	f.mu.Unlock()

	return stream.SendAndClose(&datanodev1.StoreBlockResponse{
		BlockId:   header.GetBlockId(),
		SizeBytes: int64(buf.Len()),
	})
}

func (f *fakeReplica) get(id string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.blocks[id]

	return data, ok
}

// newTestReplicator crea un Replicator cuyo dial resuelve a réplicas en memoria.
func newTestReplicator(replicas map[string]*fakeReplica) *Replicator {
	return &Replicator{
		dial: func(ctx context.Context, target string) (datanodev1.BlockServiceClient, func() error, error) {
			r, ok := replicas[target]
			if !ok {
				return nil, nil, errors.New("target desconocido: " + target)
			}

			conn, err := grpc.NewClient(
				"passthrough:///bufnet",
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
					return r.lis.DialContext(ctx)
				}),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				return nil, nil, err
			}

			return datanodev1.NewBlockServiceClient(conn), conn.Close, nil
		},
	}
}

func blockFrom(id, sha string, data []byte) Block {
	return Block{
		BlockID: id,
		SHA256:  sha,
		Size:    int64(len(data)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		},
	}
}

func TestReplicateToAllTargets(t *testing.T) {
	dnB := newFakeReplica(t)
	dnC := newFakeReplica(t)

	rep := newTestReplicator(map[string]*fakeReplica{"dnB": dnB, "dnC": dnC})

	data := []byte("bloque a replicar")
	stored, err := rep.Replicate(context.Background(), blockFrom("blk-1", "", data), []string{"dnB", "dnC"})
	if err != nil {
		t.Fatalf("Replicate() error inesperado: %v", err)
	}

	if len(stored) != 2 {
		t.Fatalf("Replicate() reportó %d réplicas, se esperaban 2", len(stored))
	}

	for name, r := range map[string]*fakeReplica{"dnB": dnB, "dnC": dnC} {
		got, ok := r.get("blk-1")
		if !ok {
			t.Fatalf("la réplica %s no almacenó el bloque", name)
		}

		if !bytes.Equal(got, data) {
			t.Fatalf("la réplica %s guardó %q, se esperaba %q", name, got, data)
		}
	}
}

func TestReplicatePartialFailure(t *testing.T) {
	dnB := newFakeReplica(t)

	// dnC no existe en el mapa: su dial fallará.
	rep := newTestReplicator(map[string]*fakeReplica{"dnB": dnB})

	data := []byte("bloque con una réplica caída")
	stored, err := rep.Replicate(context.Background(), blockFrom("blk-1", "", data), []string{"dnB", "dnC"})

	if err == nil {
		t.Fatalf("Replicate() debió reportar el fallo de dnC")
	}

	if len(stored) != 1 || stored[0] != "dnB" {
		t.Fatalf("Replicate() stored = %v, se esperaba [dnB]", stored)
	}
}
