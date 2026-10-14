package grpcserver

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/blockstore"
)

// newTestClient levanta el servidor sobre una conexión en memoria (bufconn)
// y devuelve un cliente conectado a él.
func newTestClient(t *testing.T) datanodev1.BlockServiceClient {
	t.Helper()

	disk, err := blockstore.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	datanodev1.RegisterBlockServiceServer(srv, NewServer(disk, nil))

	go func() {
		_ = srv.Serve(lis)
	}()

	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("no se pudo crear el cliente: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	return datanodev1.NewBlockServiceClient(conn)
}

// storeBlock sube un bloque usando el stream del cliente.
func storeBlock(t *testing.T, client datanodev1.BlockServiceClient, id string, data []byte) *datanodev1.StoreBlockResponse {
	t.Helper()

	stream, err := client.StoreBlock(context.Background())
	if err != nil {
		t.Fatalf("StoreBlock() error inesperado: %v", err)
	}

	header := &datanodev1.StoreBlockRequest{
		Payload: &datanodev1.StoreBlockRequest_Header{
			Header: &datanodev1.StoreBlockHeader{BlockId: id},
		},
	}

	if err := stream.Send(header); err != nil {
		t.Fatalf("Send(header) error inesperado: %v", err)
	}

	chunk := &datanodev1.StoreBlockRequest{
		Payload: &datanodev1.StoreBlockRequest_Chunk{Chunk: data},
	}

	if err := stream.Send(chunk); err != nil {
		t.Fatalf("Send(chunk) error inesperado: %v", err)
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv() error inesperado: %v", err)
	}

	return resp
}

// getBlock descarga un bloque reconstruyendo los chunks del stream.
func getBlock(t *testing.T, client datanodev1.BlockServiceClient, id string) []byte {
	t.Helper()

	stream, err := client.GetBlock(context.Background(), &datanodev1.GetBlockRequest{BlockId: id})
	if err != nil {
		t.Fatalf("GetBlock() error inesperado: %v", err)
	}

	var buf bytes.Buffer

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("Recv() error inesperado: %v", err)
		}

		if chunk := resp.GetChunk(); chunk != nil {
			buf.Write(chunk)
		}
	}

	return buf.Bytes()
}

func TestStoreAndGetBlock(t *testing.T) {
	client := newTestClient(t)

	data := []byte("contenido del bloque por gRPC")

	resp := storeBlock(t, client, "blk-1", data)
	if resp.GetSizeBytes() != int64(len(data)) {
		t.Fatalf("StoreBlock() size = %d, se esperaba %d", resp.GetSizeBytes(), len(data))
	}

	got := getBlock(t, client, "blk-1")
	if !bytes.Equal(got, data) {
		t.Fatalf("GetBlock() devolvió %q, se esperaba %q", got, data)
	}
}

func TestVerifyBlock(t *testing.T) {
	client := newTestClient(t)

	data := []byte("datos a verificar")
	resp := storeBlock(t, client, "blk-1", data)

	verify, err := client.VerifyBlock(context.Background(), &datanodev1.VerifyBlockRequest{BlockId: "blk-1"})
	if err != nil {
		t.Fatalf("VerifyBlock() error inesperado: %v", err)
	}

	if !verify.GetExists() {
		t.Fatalf("VerifyBlock() exists = false, se esperaba true")
	}

	if verify.GetSha256() != resp.GetSha256() {
		t.Fatalf("VerifyBlock() sha256 = %q, se esperaba %q", verify.GetSha256(), resp.GetSha256())
	}
}

func TestDeleteBlock(t *testing.T) {
	client := newTestClient(t)

	storeBlock(t, client, "blk-1", []byte("x"))

	resp, err := client.DeleteBlock(context.Background(), &datanodev1.DeleteBlockRequest{BlockId: "blk-1"})
	if err != nil {
		t.Fatalf("DeleteBlock() error inesperado: %v", err)
	}

	if !resp.GetDeleted() {
		t.Fatalf("DeleteBlock() deleted = false, se esperaba true")
	}

	// Un segundo borrado reporta deleted=false sin error.
	resp2, err := client.DeleteBlock(context.Background(), &datanodev1.DeleteBlockRequest{BlockId: "blk-1"})
	if err != nil {
		t.Fatalf("DeleteBlock() error inesperado: %v", err)
	}

	if resp2.GetDeleted() {
		t.Fatalf("DeleteBlock() deleted = true en bloque ya borrado")
	}
}

func TestGetMissingBlock(t *testing.T) {
	client := newTestClient(t)

	stream, err := client.GetBlock(context.Background(), &datanodev1.GetBlockRequest{BlockId: "no-existe"})
	if err != nil {
		t.Fatalf("GetBlock() error inesperado al abrir: %v", err)
	}

	if _, err := stream.Recv(); err == nil {
		t.Fatalf("GetBlock() debió fallar para un bloque inexistente")
	}
}
