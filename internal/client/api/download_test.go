package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// sha256 de los 0 bytes
const emptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func fakeGetServer(t *testing.T, resp getFileResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files/docs/a.txt" {
			t.Errorf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestDownloadRechazaArchivoGrande(t *testing.T) {
	srv := fakeGetServer(t, getFileResponse{SHA256: emptySHA, SizeBytes: 10 << 20})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	_, err := newTestClient(srv.URL).Download("/docs/a.txt", dest, "tok", 5<<20)

	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("se esperaba *TooLargeError, llegó: %v", err)
	}
	if tooLarge.Size != 10<<20 || tooLarge.Max != 5<<20 {
		t.Fatalf("datos inesperados: %+v", tooLarge)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("no debía crearse el archivo de destino")
	}
}

func TestDownloadDevuelveLaVersionDescargada(t *testing.T) {
	srv := fakeGetServer(t, getFileResponse{SHA256: emptySHA, SizeBytes: 0})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	info, err := newTestClient(srv.URL).Download("/docs/a.txt", dest, "tok", 1024)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	if info.SHA256 != emptySHA || info.SizeBytes != 0 {
		t.Fatalf("info inesperada: %+v", info)
	}

	st, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("el archivo debía existir: %v", err)
	}
	if runtimePerm := st.Mode().Perm(); runtimePerm != 0o600 {
		t.Fatalf("permisos = %o, se esperaba 600", runtimePerm)
	}
}

func TestDownloadSinLimiteAceptaCualquierTamano(t *testing.T) {
	srv := fakeGetServer(t, getFileResponse{SHA256: emptySHA, SizeBytes: 1 << 40})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	if _, err := newTestClient(srv.URL).Download("/docs/a.txt", dest, "tok", 0); err != nil {
		t.Fatalf("sin límite no debía rechazar: %v", err)
	}
}

func TestReplaceEnviaLaVersionBaseYPropaga409(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/put-init" {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "el archivo cambió desde que se descargó"})
	}))
	defer srv.Close()

	local := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(local, []byte("hola"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := newTestClient(srv.URL).Replace(local, "/docs/a.txt", "abc123", "tok")

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("se esperaba *Error 409, llegó: %v", err)
	}
	if got["replace"] != true || got["base_sha256"] != "abc123" || got["path"] != "/docs/a.txt" {
		t.Fatalf("put-init mal formado: %+v", got)
	}
}

func TestPutNoEnviaCamposDeReemplazo(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "ya existe"})
	}))
	defer srv.Close()

	local := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(local, []byte("hola"), 0o600); err != nil {
		t.Fatal(err)
	}

	_ = newTestClient(srv.URL).Put(local, "/docs/a.txt", "tok")

	if _, ok := got["replace"]; ok {
		t.Fatalf("Put no debe enviar replace: %+v", got)
	}
	if _, ok := got["base_sha256"]; ok {
		t.Fatalf("Put no debe enviar base_sha256: %+v", got)
	}
}
