package shell

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"dfsha/internal/client/api"
	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/datanode/crypto"
	"dfsha/internal/server/datanode/grpcserver"
	"dfsha/internal/server/datanode/heartbeat"
	"dfsha/internal/server/datanode/replication"
	"dfsha/internal/server/metanode/api/rest"
	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

const intClusterSecret = "cluster-integration"

// cluster es un Metanodo REST con DataNodes gRPC reales (cifrado, tokens y secreto de clúster).
type cluster struct {
	store     *store.MemoryStore
	client    *api.Client
	stores    map[string]blockstore.BlockStore
	reporters map[string]*heartbeat.HTTPReporter
}

func startCluster(t *testing.T, nodes int) *cluster {
	t.Helper()
	t.Setenv("METANODE_JWT_SECRET", "jwt-integration")
	t.Setenv("DFSHA_CAPABILITY_SECRET", "cap-integration")

	st := store.NewMemoryStore()
	srv := rest.NewServer(st)
	srv.SetClusterSecret(intClusterSecret)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	t.Setenv("DFSHA_METANODE_URL", ts.URL)

	c := &cluster{
		store:     st,
		stores:    map[string]blockstore.BlockStore{},
		reporters: map[string]*heartbeat.HTTPReporter{},
	}

	key := bytes.Repeat([]byte{7}, crypto.KeySize)
	for i := 0; i < nodes; i++ {
		id := fmt.Sprintf("dn%d", i+1)

		disk, err := blockstore.NewDiskStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cipher, err := crypto.NewAESGCM(key)
		if err != nil {
			t.Fatal(err)
		}
		bs := blockstore.NewEncryptedBlockStore(disk, cipher)
		c.stores[id] = bs

		rep := heartbeat.NewHTTPReporter(ts.URL)
		rep.SetClusterSecret(intClusterSecret)
		c.reporters[id] = rep

		repl := replication.NewReplicator()
		repl.SetClusterSecret(intClusterSecret)

		g := grpcserver.NewServer(bs, repl).
			WithAuth(auth.NewValidator([]byte("cap-integration")), intClusterSecret, id).
			WithReporter(rep)
		gs := grpc.NewServer()
		datanodev1.RegisterBlockServiceServer(gs, g)

		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go gs.Serve(lis)
		t.Cleanup(gs.Stop)

		if err := service.RegisterDataNode(st, id, lis.Addr().String()); err != nil {
			t.Fatal(err)
		}
	}

	c.client = api.New()
	return c
}

// login registra al usuario y devuelve su token de sesión.
func (c *cluster) login(t *testing.T, user string) string {
	t.Helper()
	if err := c.client.Register(user, "secreta1"); err != nil {
		t.Fatalf("register: %v", err)
	}
	res, err := c.client.Login(user, "secreta1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return res.Token
}

// beats simula un latido de cada DataNode y aplica los borrados que ordena el Metanodo.
func (c *cluster) beats(t *testing.T) {
	t.Helper()
	for id, rep := range c.reporters {
		toDelete, err := rep.Beat(context.Background(), heartbeat.Report{DataNodeID: id, Address: "x:1"})
		if err != nil {
			t.Fatalf("beat %s: %v", id, err)
		}
		for _, blockID := range toDelete {
			_, _ = c.stores[id].Delete(blockID)
		}
	}
}

func (c *cluster) totalBlocks(t *testing.T) int {
	t.Helper()
	total := 0
	for _, bs := range c.stores {
		list, err := bs.List()
		if err != nil {
			t.Fatal(err)
		}
		total += len(list)
	}
	return total
}

func (c *cluster) quota(t *testing.T, user string) int64 {
	t.Helper()
	u, err := c.store.GetUser(user)
	if err != nil {
		t.Fatal(err)
	}
	return u.QuotaUsedBytes
}

func writeLocal(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIntegracionCatYEdit(t *testing.T) {
	tmp := useTempDir(t)
	c := startCluster(t, 4)
	tok := c.login(t, "ana")

	if err := c.client.Put(writeLocal(t, "linea uno\nlinea dos\n"), "/notas.txt", tok); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := c.totalBlocks(t); got != 3 {
		t.Fatalf("tras el put debía haber 3 réplicas de 1 bloque, hay %d", got)
	}

	// cat muestra lo subido y no deja temporales.
	var out bytes.Buffer
	if err := catFile(c.client, tok, "/notas.txt", &out); err != nil {
		t.Fatalf("cat: %v", err)
	}
	if out.String() != "linea uno\nlinea dos\n" {
		t.Fatalf("cat = %q", out.String())
	}
	assertEmptyDir(t, tmp)

	// edit reemplaza el contenido.
	out.Reset()
	err := editFile(c.client, tok, "/notas.txt", []string{"x"},
		editorThat(func(s string) string { return s + "linea tres\n" }), &out)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	assertEmptyDir(t, tmp)

	out.Reset()
	if err := catFile(c.client, tok, "/notas.txt", &out); err != nil {
		t.Fatalf("cat tras editar: %v", err)
	}
	if out.String() != "linea uno\nlinea dos\nlinea tres\n" {
		t.Fatalf("cat tras editar = %q", out.String())
	}

	// El bloque viejo se borra en los DataNodes al llegar el siguiente latido.
	c.beats(t)
	if got := c.totalBlocks(t); got != 3 {
		t.Fatalf("tras el reemplazo debían quedar 3 réplicas de 1 bloque, hay %d", got)
	}
	if got := c.quota(t, "ana"); got != int64(len("linea uno\nlinea dos\nlinea tres\n")) {
		t.Fatalf("cuota = %d", got)
	}
}

func TestIntegracionEditArchivoDeVariosBloques(t *testing.T) {
	useTempDir(t)
	c := startCluster(t, 4)
	tok := c.login(t, "ana")

	// 2,5 MiB de texto: 3 bloques de 1 MiB.
	original := strings.Repeat("0123456789abcdef\n", (5<<19)/17)
	if err := c.client.Put(writeLocal(t, original), "/grande.txt", tok); err != nil {
		t.Fatalf("put: %v", err)
	}

	var out bytes.Buffer
	err := editFile(c.client, tok, "/grande.txt", []string{"x"},
		editorThat(func(s string) string { return "inicio\n" + s }), &out)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "copia.txt")
	if err := c.client.Get("/grande.txt", dest, tok); err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "inicio\n"+original {
		t.Fatalf("el archivo descargado no coincide con lo editado (%d bytes)", len(got))
	}

	c.beats(t)
	files, _ := c.store.GetFile("ana", "/grande.txt")
	if want := len(files.BlockIDs) * 3; c.totalBlocks(t) != want {
		t.Fatalf("bloques en DataNodes = %d, se esperaban %d", c.totalBlocks(t), want)
	}
}

func TestIntegracionEditCreaArchivoNuevo(t *testing.T) {
	useTempDir(t)
	c := startCluster(t, 3)
	tok := c.login(t, "ana")

	var out bytes.Buffer
	err := editFile(c.client, tok, "/nuevo.txt", []string{"x"},
		editorThat(func(string) string { return "creado desde edit\n" }), &out)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	out.Reset()
	if err := catFile(c.client, tok, "/nuevo.txt", &out); err != nil || out.String() != "creado desde edit\n" {
		t.Fatalf("cat = %q (err=%v)", out.String(), err)
	}
}

func TestIntegracionConflictoNoPierdeElTrabajo(t *testing.T) {
	useTempDir(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	c := startCluster(t, 3)
	tok := c.login(t, "ana")
	if err := c.client.Put(writeLocal(t, "base\n"), "/c.txt", tok); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Otra sesión guarda su versión mientras esta edita.
	editor := func(argv []string, file string) error {
		other := writeLocal(t, "version de la otra sesion\n")
		if err := c.client.Replace(other, "/c.txt", sha([]byte("base\n")), tok); err != nil {
			t.Errorf("la otra sesión no pudo guardar: %v", err)
		}
		return os.WriteFile(file, []byte("mi version\n"), 0o600)
	}

	var out bytes.Buffer
	err := editFile(c.client, tok, "/c.txt", []string{"x"}, editor, &out)
	if err == nil || !strings.Contains(err.Error(), "changed on the server") {
		t.Fatalf("error = %v, se esperaba aviso de conflicto", err)
	}

	// Mi trabajo quedó en una copia de recuperación; el servidor conserva la otra versión.
	saved, rerr := os.ReadFile(filepath.Join(cwd, "c.txt.dfsha-recovery"))
	if rerr != nil || string(saved) != "mi version\n" {
		t.Fatalf("copia de recuperación = %q (err=%v)", saved, rerr)
	}
	out.Reset()
	if err := catFile(c.client, tok, "/c.txt", &out); err != nil || out.String() != "version de la otra sesion\n" {
		t.Fatalf("el servidor debía conservar la otra versión: %q (err=%v)", out.String(), err)
	}
}

func TestIntegracionCatRechazaBinarioYGrande(t *testing.T) {
	useTempDir(t)
	c := startCluster(t, 3)
	tok := c.login(t, "ana")

	bin := filepath.Join(t.TempDir(), "b.bin")
	if err := os.WriteFile(bin, []byte("abc\x00def"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.client.Put(bin, "/b.bin", tok); err != nil {
		t.Fatalf("put binario: %v", err)
	}

	var out bytes.Buffer
	if err := catFile(c.client, tok, "/b.bin", &out); err == nil || out.Len() != 0 {
		t.Fatalf("cat debía rechazar el binario sin imprimir (err=%v, out=%q)", err, out.String())
	}
	if err := editFile(c.client, tok, "/b.bin", []string{"x"}, neverRun(t), &out); err == nil {
		t.Fatalf("edit debía rechazar el binario")
	}

	// Archivo mayor que el tope de edit: se rechaza sin abrir el editor.
	bigPath := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(bigPath, bytes.Repeat([]byte("a"), maxEditBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.client.Put(bigPath, "/big.txt", tok); err != nil {
		t.Fatalf("put grande: %v", err)
	}
	err := editFile(c.client, tok, "/big.txt", []string{"x"}, neverRun(t), &out)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("edit de archivo grande: error = %v", err)
	}
}

func TestIntegracionOtroUsuarioNoVeNiEditaArchivosAjenos(t *testing.T) {
	useTempDir(t)
	c := startCluster(t, 3)
	ana := c.login(t, "ana")
	bob := c.login(t, "bob")

	if err := c.client.Put(writeLocal(t, "secreto de ana\n"), "/privado.txt", ana); err != nil {
		t.Fatalf("put: %v", err)
	}

	var out bytes.Buffer
	if err := catFile(c.client, bob, "/privado.txt", &out); !isNotFound(err) {
		t.Fatalf("bob no debe ver el archivo de ana: err=%v out=%q", err, out.String())
	}

	// Para bob, /privado.txt no existe: edit lo crea en su propio espacio y no toca el de ana.
	err := editFile(c.client, bob, "/privado.txt", []string{"x"},
		editorThat(func(string) string { return "de bob\n" }), &out)
	if err != nil {
		t.Fatalf("edit de bob: %v", err)
	}
	out.Reset()
	if err := catFile(c.client, ana, "/privado.txt", &out); err != nil || out.String() != "secreto de ana\n" {
		t.Fatalf("el archivo de ana cambió: %q (err=%v)", out.String(), err)
	}
}
