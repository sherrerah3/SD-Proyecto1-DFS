package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

const testClusterSecret = "cluster-secret-de-prueba"

// setup arma un servidor REST de prueba sobre un MemoryStore con datanodes vivos.
func setup(t *testing.T) (http.Handler, store.Store) {
	t.Helper()
	t.Setenv("METANODE_JWT_SECRET", "test-secret")
	t.Setenv("DFSHA_CAPABILITY_SECRET", "test-capability-secret")

	s := store.NewMemoryStore()
	for i, id := range []string{"dn1", "dn2", "dn3"} {
		if err := service.RegisterDataNode(s, id, "127.0.0.1:900"+string(rune('0'+i))); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}
	srv := NewServer(s)
	srv.SetClusterSecret(testClusterSecret)
	return srv.Routes(), s
}

// alcanzarQuorumAPI reporta cada bloque desde dos DataNodes
func alcanzarQuorumAPI(t *testing.T, h http.Handler, initBody []byte, sha string) {
	t.Helper()
	var init struct {
		Blocks []struct {
			BlockID string `json:"block_id"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(initBody, &init); err != nil {
		t.Fatalf("failed to read the put-init response: %v", err)
	}
	for _, b := range init.Blocks {
		for _, dn := range []string{"dn1", "dn2"} {
			rec := doCluster(t, h, "POST", "/internal/blocks/report", map[string]string{
				"block_id": b.BlockID, "datanode_id": dn, "sha256": sha,
			})
			if rec.Code != http.StatusNoContent {
				t.Fatalf("block report returned %d: %s", rec.Code, rec.Body.String())
			}
		}
	}
}

// doCluster envía una petición con el header del secreto de clúster.
func doCluster(t *testing.T, h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set("X-Cluster-Secret", testClusterSecret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// do envía una petición y devuelve la respuesta grabada.
func do(t *testing.T, h http.Handler, method, target, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// registraYLoguea crea un usuario y devuelve su token de sesión.
func registraYLoguea(t *testing.T, h http.Handler, user, pass string) string {
	t.Helper()
	creds := map[string]string{"username": user, "password": pass}
	if rec := do(t, h, "POST", "/auth/register", "", creds); rec.Code != http.StatusNoContent {
		t.Fatalf("register returned %d", rec.Code)
	}
	rec := do(t, h, "POST", "/auth/login", "", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login returned %d", rec.Code)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to read the token: %v", err)
	}
	return resp.Token
}

func TestLoginMalasCredencialesMismaRespuesta(t *testing.T) {
	h, _ := setup(t)
	registraYLoguea(t, h, "ana", "secreta1")

	malPass := do(t, h, "POST", "/auth/login", "", map[string]string{"username": "ana", "password": "incorrecta"})
	noExiste := do(t, h, "POST", "/auth/login", "", map[string]string{"username": "nadie", "password": "secreta1"})

	if malPass.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d, expected 401", malPass.Code)
	}
	if noExiste.Code != http.StatusUnauthorized {
		t.Fatalf("missing user: status code %d, expected 401", noExiste.Code)
	}
	if malPass.Body.String() != noExiste.Body.String() {
		t.Fatalf("responses differ and reveal whether the user exists:\n%q\n%q",
			malPass.Body.String(), noExiste.Body.String())
	}
}

func TestRutaProtegidaSinOConTokenInvalido(t *testing.T) {
	h, _ := setup(t)

	sinHeader := do(t, h, "GET", "/files/a.txt", "", nil)
	if sinHeader.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: status code %d, expected 401", sinHeader.Code)
	}

	manipulado := do(t, h, "GET", "/files/a.txt", "token.falso.xyz", nil)
	if manipulado.Code != http.StatusUnauthorized {
		t.Fatalf("tampered token: status code %d, expected 401", manipulado.Code)
	}
}

func TestCapabilityTokenNoSirveComoSesion(t *testing.T) {
	h, _ := setup(t)

	capTok, err := service.GenerateCapabilityToken("ana", service.OpGet, []string{"blk-1"}, service.SessionTTL)
	if err != nil {
		t.Fatalf("failed to generate capability token: %v", err)
	}

	rec := do(t, h, "GET", "/files/a.txt", capTok, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("capability token used as a session: status code %d, expected 401", rec.Code)
	}
}

func TestPutInitEntradasInvalidas(t *testing.T) {
	h, _ := setup(t)
	token := registraYLoguea(t, h, "ana", "secreta1")

	negativo := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "/a.txt", "size_bytes": -1})
	if negativo.Code != http.StatusBadRequest {
		t.Fatalf("negative size: status code %d, expected 400", negativo.Code)
	}

	relativa := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "a.txt", "size_bytes": 1024})
	if relativa.Code != http.StatusBadRequest {
		t.Fatalf("relative path: status %d, expected 400", relativa.Code)
	}
}

func TestArchivoAjenoDa404(t *testing.T) {
	h, _ := setup(t)
	tokenA := registraYLoguea(t, h, "aaa", "secreta1")
	tokenB := registraYLoguea(t, h, "bbb", "secreta1")

	// A sube y completa un archivo.
	init := do(t, h, "POST", "/files/put-init", tokenA, map[string]any{"path": "/a.txt", "size_bytes": 1024})
	if init.Code != http.StatusOK {
		t.Fatalf("put-init for A returned %d", init.Code)
	}
	sha := strings.Repeat("0", 64)
	alcanzarQuorumAPI(t, h, init.Body.Bytes(), sha)
	complete := do(t, h, "POST", "/files/put-complete", tokenA, map[string]any{"path": "/a.txt", "sha256": sha})
	if complete.Code != http.StatusNoContent {
		t.Fatalf("put-complete for A returned %d", complete.Code)
	}

	// B intenta GET y DELETE sobre el archivo de A: ambos 404.
	if get := do(t, h, "GET", "/files/a.txt", tokenB, nil); get.Code != http.StatusNotFound {
		t.Fatalf("GET for another user's file: status code %d, expected 404", get.Code)
	}
	if del := do(t, h, "DELETE", "/files/a.txt", tokenB, nil); del.Code != http.StatusNotFound {
		t.Fatalf("DELETE for another user's file: status code %d, expected 404", del.Code)
	}
}

func TestPutCompleteDosVecesDa409(t *testing.T) {
	h, _ := setup(t)
	token := registraYLoguea(t, h, "ana", "secreta1")

	init := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "/a.txt", "size_bytes": 1024})
	if init.Code != http.StatusOK {
		t.Fatalf("put-init yielded %d", init.Code)
	}
	sha := strings.Repeat("0", 64)
	alcanzarQuorumAPI(t, h, init.Body.Bytes(), sha)
	body := map[string]any{"path": "/a.txt", "sha256": sha}

	if primero := do(t, h, "POST", "/files/put-complete", token, body); primero.Code != http.StatusNoContent {
		t.Fatalf("first put-complete yielded %d", primero.Code)
	}
	segundo := do(t, h, "POST", "/files/put-complete", token, body)
	if segundo.Code != http.StatusConflict {
		t.Fatalf("second put-complete: status %d, expected 409", segundo.Code)
	}
}
