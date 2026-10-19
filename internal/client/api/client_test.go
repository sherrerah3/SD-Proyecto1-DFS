package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(urls ...string) *Client {
	return &Client{baseURLs: urls, http: &http.Client{}}
}

func TestLoginOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/login" {
			t.Errorf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}

		var body credentials
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("invalid body: %v", err)
		}

		if body.Username != "ana" || body.Password != "clave123" {
			t.Errorf("credenciales inesperadas: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(LoginResult{Token: "tok", ExpiresIn: 60})
	}))
	defer srv.Close()

	result, err := newTestClient(srv.URL).Login("ana", "clave123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if result.Token != "tok" || result.ExpiresIn != 60 {
		t.Fatalf("resultado unexpected: %+v", result)
	}
}

func TestRegisterNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/register" {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv.URL).Register("ana", "clave123"); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

func TestServerErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Login("ana", "mala")

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got: %v", err)
	}

	if apiErr.Status != http.StatusUnauthorized || apiErr.Message != "invalid credentials" {
		t.Fatalf("error unexpected: %+v", apiErr)
	}
}

func TestFallsBackToNextMetanode(t *testing.T) {
	// El primer metanode está caído: se abre y se cierra para obtener una URL sin servidor.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer alive.Close()

	if err := newTestClient(deadURL, alive.URL).Register("ana", "clave123"); err != nil {
		t.Fatalf("should have used the second MetaNode: %v", err)
	}
}

func TestAllMetanodesDown(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	if err := newTestClient(deadURL).Register("ana", "clave123"); err == nil {
		t.Fatal("expected an error when no MetaNode is available")
	}
}
