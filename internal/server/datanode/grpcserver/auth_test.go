package grpcserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
	"dfsha/internal/server/datanode/blockstore"
)

const (
	capSecret     = "cap-secret-test"
	clusterSecret = "cluster-secret-test"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fakeReporter guarda los bloques que el DataNode reporta al Metanodo.
type fakeReporter struct {
	mu      sync.Mutex
	reports []string
}

func (f *fakeReporter) ReportBlock(_ context.Context, blockID, dataNodeID, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, blockID+"|"+dataNodeID+"|"+sha)
	return nil
}

// newSecureClient levanta un DataNode con autenticación activada.
func newSecureClient(t *testing.T) (datanodev1.BlockServiceClient, *fakeReporter) {
	t.Helper()

	disk, err := blockstore.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	reporter := &fakeReporter{}
	server := NewServer(disk, nil).
		WithAuth(auth.NewValidator([]byte(capSecret)), clusterSecret, "dn1").
		WithReporter(reporter)

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	datanodev1.RegisterBlockServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

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

	return datanodev1.NewBlockServiceClient(conn), reporter
}

// capToken firma un capability token; secret y claims permiten fabricar tokens inválidos.
func capToken(t *testing.T, secret, typ, op string, blocks []string, ttl time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":       "ana",
		"typ":       typ,
		"op":        op,
		"block_ids": blocks,
		"exp":       time.Now().Add(ttl).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("no se pudo firmar el token: %v", err)
	}
	return signed
}

func withToken(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
}

func withCluster(secret string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "x-cluster-secret", secret)
}

// storeWith sube un bloque con el contexto dado y devuelve el error del servidor.
func storeWith(ctx context.Context, client datanodev1.BlockServiceClient, id string, data []byte) error {
	stream, err := client.StoreBlock(ctx)
	if err != nil {
		return err
	}

	header := &datanodev1.StoreBlockRequest{
		Payload: &datanodev1.StoreBlockRequest_Header{Header: &datanodev1.StoreBlockHeader{BlockId: id}},
	}
	// Si el servidor ya rechazó, Send puede devolver EOF: el error real llega en CloseAndRecv.
	_ = stream.Send(header)
	_ = stream.Send(&datanodev1.StoreBlockRequest{Payload: &datanodev1.StoreBlockRequest_Chunk{Chunk: data}})

	_, err = stream.CloseAndRecv()
	return err
}

// readWith descarga un bloque con el contexto dado.
func readWith(ctx context.Context, client datanodev1.BlockServiceClient, id string) ([]byte, error) {
	stream, err := client.GetBlock(ctx, &datanodev1.GetBlockRequest{BlockId: id})
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		buf.Write(resp.GetChunk())
	}
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("código = %v (err=%v), se esperaba %v", got, err, want)
	}
}

func TestStoreBlockConTokenValidoYReporte(t *testing.T) {
	client, reporter := newSecureClient(t)

	token := capToken(t, capSecret, "capability", "put", []string{"blk-1"}, time.Minute)
	if err := storeWith(withToken(token), client, "blk-1", []byte("datos")); err != nil {
		t.Fatalf("StoreBlock con token válido falló: %v", err)
	}

	if len(reporter.reports) != 1 {
		t.Fatalf("se esperaba 1 reporte al Metanodo, hay %d", len(reporter.reports))
	}
	// El reporte lleva el bloque, el nodo que lo guardó y el hash del contenido.
	want := "blk-1|dn1|" + sha256Hex([]byte("datos"))
	if reporter.reports[0] != want {
		t.Fatalf("reporte = %q, se esperaba %q", reporter.reports[0], want)
	}
}

func TestStoreBlockRechazaTokensInvalidos(t *testing.T) {
	tests := []struct {
		name  string
		ctx   func(t *testing.T) context.Context
		block string
		want  codes.Code
	}{
		{
			name: "sin token",
			ctx:  func(*testing.T) context.Context { return context.Background() },
			want: codes.Unauthenticated,
		},
		{
			name: "token de lectura no sirve para escribir",
			ctx: func(t *testing.T) context.Context {
				return withToken(capToken(t, capSecret, "capability", "get", []string{"blk-1"}, time.Minute))
			},
			want: codes.PermissionDenied,
		},
		{
			name: "token de otro bloque",
			ctx: func(t *testing.T) context.Context {
				return withToken(capToken(t, capSecret, "capability", "put", []string{"otro"}, time.Minute))
			},
			want: codes.PermissionDenied,
		},
		{
			name: "token vencido",
			ctx: func(t *testing.T) context.Context {
				return withToken(capToken(t, capSecret, "capability", "put", []string{"blk-1"}, -time.Minute))
			},
			want: codes.PermissionDenied,
		},
		{
			name: "firmado con otra clave",
			ctx: func(t *testing.T) context.Context {
				return withToken(capToken(t, "clave-ajena", "capability", "put", []string{"blk-1"}, time.Minute))
			},
			want: codes.PermissionDenied,
		},
		{
			name: "token de sesión",
			ctx: func(t *testing.T) context.Context {
				return withToken(capToken(t, capSecret, "session", "put", []string{"blk-1"}, time.Minute))
			},
			want: codes.PermissionDenied,
		},
		{
			name: "secreto de clúster incorrecto",
			ctx:  func(*testing.T) context.Context { return withCluster("incorrecto") },
			want: codes.Unauthenticated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, reporter := newSecureClient(t)

			err := storeWith(tc.ctx(t), client, "blk-1", []byte("datos"))
			assertCode(t, err, tc.want)

			if len(reporter.reports) != 0 {
				t.Fatalf("un bloque rechazado no debe reportarse")
			}
			// Y tampoco debe quedar escrito.
			verify, verr := client.VerifyBlock(context.Background(), &datanodev1.VerifyBlockRequest{BlockId: "blk-1"})
			if verr != nil || verify.GetExists() {
				t.Fatalf("el bloque rechazado no debía existir (err=%v)", verr)
			}
		})
	}
}

func TestStoreBlockAceptaReplicaConSecretoDeCluster(t *testing.T) {
	client, _ := newSecureClient(t)

	if err := storeWith(withCluster(clusterSecret), client, "blk-1", []byte("replica")); err != nil {
		t.Fatalf("la réplica con secreto de clúster debía aceptarse: %v", err)
	}
}

func TestGetBlockExigeTokenDeLectura(t *testing.T) {
	client, _ := newSecureClient(t)

	put := capToken(t, capSecret, "capability", "put", []string{"blk-1"}, time.Minute)
	if err := storeWith(withToken(put), client, "blk-1", []byte("contenido")); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	get := capToken(t, capSecret, "capability", "get", []string{"blk-1"}, time.Minute)
	got, err := readWith(withToken(get), client, "blk-1")
	if err != nil || string(got) != "contenido" {
		t.Fatalf("lectura con token válido: %q (err=%v)", got, err)
	}

	_, err = readWith(context.Background(), client, "blk-1")
	assertCode(t, err, codes.Unauthenticated)

	_, err = readWith(withToken(put), client, "blk-1")
	assertCode(t, err, codes.PermissionDenied)

	other := capToken(t, capSecret, "capability", "get", []string{"otro"}, time.Minute)
	_, err = readWith(withToken(other), client, "blk-1")
	assertCode(t, err, codes.PermissionDenied)
}

func TestDeleteBlockSoloConSecretoDeCluster(t *testing.T) {
	client, _ := newSecureClient(t)

	put := capToken(t, capSecret, "capability", "put", []string{"blk-1"}, time.Minute)
	if err := storeWith(withToken(put), client, "blk-1", []byte("x")); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	req := &datanodev1.DeleteBlockRequest{BlockId: "blk-1"}

	// Ni sin credenciales, ni con un token de cliente, ni con otro secreto se puede borrar.
	_, err := client.DeleteBlock(context.Background(), req)
	assertCode(t, err, codes.PermissionDenied)
	_, err = client.DeleteBlock(withToken(put), req)
	assertCode(t, err, codes.PermissionDenied)
	_, err = client.DeleteBlock(withCluster("incorrecto"), req)
	assertCode(t, err, codes.PermissionDenied)

	resp, err := client.DeleteBlock(withCluster(clusterSecret), req)
	if err != nil || !resp.GetDeleted() {
		t.Fatalf("el borrado con secreto de clúster debía funcionar (err=%v)", err)
	}
}

func TestStoreBlockConHashIncorrectoBorraElBloque(t *testing.T) {
	client := newTestClient(t)

	stream, err := client.StoreBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	header := &datanodev1.StoreBlockHeader{BlockId: "blk-1", Sha256: sha256Hex([]byte("otro contenido"))}
	_ = stream.Send(&datanodev1.StoreBlockRequest{Payload: &datanodev1.StoreBlockRequest_Header{Header: header}})
	_ = stream.Send(&datanodev1.StoreBlockRequest{Payload: &datanodev1.StoreBlockRequest_Chunk{Chunk: []byte("contenido")}})

	_, err = stream.CloseAndRecv()
	assertCode(t, err, codes.DataLoss)

	verify, err := client.VerifyBlock(context.Background(), &datanodev1.VerifyBlockRequest{BlockId: "blk-1"})
	if err != nil || verify.GetExists() {
		t.Fatalf("el bloque corrupto no debía quedar en disco (err=%v)", err)
	}
}
