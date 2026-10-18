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
			t.Fatalf("preparación falló: %v", err)
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
		t.Fatalf("no se pudo leer la respuesta de put-init: %v", err)
	}
	for _, b := range init.Blocks {
		for _, dn := range []string{"dn1", "dn2"} {
			rec := doCluster(t, h, "POST", "/internal/blocks/report", map[string]string{
				"block_id": b.BlockID, "datanode_id": dn, "sha256": sha,
			})
			if rec.Code != http.StatusNoContent {
				t.Fatalf("report de bloque devolvió %d: %s", rec.Code, rec.Body.String())
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
		t.Fatalf("register devolvió %d", rec.Code)
	}
	rec := do(t, h, "POST", "/auth/login", "", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login devolvió %d", rec.Code)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("no se pudo leer el token: %v", err)
	}
	return resp.Token
}

func TestLoginMalasCredencialesMismaRespuesta(t *testing.T) {
	h, _ := setup(t)
	registraYLoguea(t, h, "ana", "secreta1")

	malPass := do(t, h, "POST", "/auth/login", "", map[string]string{"username": "ana", "password": "incorrecta"})
	noExiste := do(t, h, "POST", "/auth/login", "", map[string]string{"username": "nadie", "password": "secreta1"})

	if malPass.Code != http.StatusUnauthorized {
		t.Fatalf("password mala: código %d, se esperaba 401", malPass.Code)
	}
	if noExiste.Code != http.StatusUnauthorized {
		t.Fatalf("usuario inexistente: código %d, se esperaba 401", noExiste.Code)
	}
	if malPass.Body.String() != noExiste.Body.String() {
		t.Fatalf("las respuestas difieren y revelan si el usuario existe:\n%q\n%q",
			malPass.Body.String(), noExiste.Body.String())
	}
}

func TestRutaProtegidaSinOConTokenInvalido(t *testing.T) {
	h, _ := setup(t)

	sinHeader := do(t, h, "GET", "/files/a.txt", "", nil)
	if sinHeader.Code != http.StatusUnauthorized {
		t.Fatalf("sin header: código %d, se esperaba 401", sinHeader.Code)
	}

	manipulado := do(t, h, "GET", "/files/a.txt", "token.falso.xyz", nil)
	if manipulado.Code != http.StatusUnauthorized {
		t.Fatalf("token manipulado: código %d, se esperaba 401", manipulado.Code)
	}
}

func TestCapabilityTokenNoSirveComoSesion(t *testing.T) {
	h, _ := setup(t)

	capTok, err := service.GenerateCapabilityToken("ana", service.OpGet, []string{"blk-1"}, service.SessionTTL)
	if err != nil {
		t.Fatalf("no se pudo generar capability token: %v", err)
	}

	rec := do(t, h, "GET", "/files/a.txt", capTok, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("capability token como sesión: código %d, se esperaba 401", rec.Code)
	}
}

func TestPutInitEntradasInvalidas(t *testing.T) {
	h, _ := setup(t)
	token := registraYLoguea(t, h, "ana", "secreta1")

	negativo := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "/a.txt", "size_bytes": -1})
	if negativo.Code != http.StatusBadRequest {
		t.Fatalf("size negativo: código %d, se esperaba 400", negativo.Code)
	}

	relativa := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "a.txt", "size_bytes": 1024})
	if relativa.Code != http.StatusBadRequest {
		t.Fatalf("ruta relativa: código %d, se esperaba 400", relativa.Code)
	}
}

func TestArchivoAjenoDa404(t *testing.T) {
	h, _ := setup(t)
	tokenA := registraYLoguea(t, h, "aaa", "secreta1")
	tokenB := registraYLoguea(t, h, "bbb", "secreta1")

	// A sube y completa un archivo.
	init := do(t, h, "POST", "/files/put-init", tokenA, map[string]any{"path": "/a.txt", "size_bytes": 1024})
	if init.Code != http.StatusOK {
		t.Fatalf("put-init de A devolvió %d", init.Code)
	}
	sha := strings.Repeat("0", 64)
	alcanzarQuorumAPI(t, h, init.Body.Bytes(), sha)
	complete := do(t, h, "POST", "/files/put-complete", tokenA, map[string]any{"path": "/a.txt", "sha256": sha})
	if complete.Code != http.StatusNoContent {
		t.Fatalf("put-complete de A devolvió %d", complete.Code)
	}

	// B intenta GET y DELETE sobre el archivo de A: ambos 404.
	if get := do(t, h, "GET", "/files/a.txt", tokenB, nil); get.Code != http.StatusNotFound {
		t.Fatalf("GET ajeno: código %d, se esperaba 404", get.Code)
	}
	if del := do(t, h, "DELETE", "/files/a.txt", tokenB, nil); del.Code != http.StatusNotFound {
		t.Fatalf("DELETE ajeno: código %d, se esperaba 404", del.Code)
	}
}

func TestPutCompleteDosVecesDa409(t *testing.T) {
	h, _ := setup(t)
	token := registraYLoguea(t, h, "ana", "secreta1")

	init := do(t, h, "POST", "/files/put-init", token, map[string]any{"path": "/a.txt", "size_bytes": 1024})
	if init.Code != http.StatusOK {
		t.Fatalf("put-init devolvió %d", init.Code)
	}
	sha := strings.Repeat("0", 64)
	alcanzarQuorumAPI(t, h, init.Body.Bytes(), sha)
	body := map[string]any{"path": "/a.txt", "sha256": sha}

	if primero := do(t, h, "POST", "/files/put-complete", token, body); primero.Code != http.StatusNoContent {
		t.Fatalf("primer put-complete devolvió %d", primero.Code)
	}
	segundo := do(t, h, "POST", "/files/put-complete", token, body)
	if segundo.Code != http.StatusConflict {
		t.Fatalf("segundo put-complete: código %d, se esperaba 409", segundo.Code)
	}
}
