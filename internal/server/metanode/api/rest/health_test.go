package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"dfsha/internal/server/metanode/store"
)

func TestLivenessIsHealthyForStandby(t *testing.T) {
	srv := NewServer(store.NewMemoryStore())
	srv.SetLeaderCheck(func() bool { return false })
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	srv.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestLeaderHealthRejectsStandby(t *testing.T) {
	srv := NewServer(store.NewMemoryStore())
	srv.SetLeaderCheck(func() bool { return false })
	request := httptest.NewRequest(http.MethodGet, "/health/leader", nil)
	response := httptest.NewRecorder()
	srv.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("leader health status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestLeaderHealthAcceptsLeader(t *testing.T) {
	srv := NewServer(store.NewMemoryStore())
	srv.SetLeaderCheck(func() bool { return true })
	request := httptest.NewRequest(http.MethodGet, "/health/leader", nil)
	response := httptest.NewRecorder()
	srv.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("leader health status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}
