package rest

import (
	"log"
	"net/http"
	"sync"
	"time"

	"dfsha/internal/server/metanode/store"
)

type Server struct {
	store         store.Store
	clusterSecret string
	isLeader      func() bool // nil = siempre líder
	lockMu        sync.Mutex
	locks         map[string]*fileLock
	nextFence     uint64
}

func NewServer(s store.Store) *Server {
	return &Server{
		store: s,
		locks: make(map[string]*fileLock),
	}
}

// SetClusterSecret fija el secreto de las rutas /internal/*.
func (srv *Server) SetClusterSecret(secret string) {
	srv.clusterSecret = secret
}

// SetLeaderCheck indica si este nodo es el líder.
func (srv *Server) SetLeaderCheck(fn func() bool) {
	srv.isLeader = fn
}

func (srv *Server) leader() bool {
	return srv.isLeader == nil || srv.isLeader()
}

// Routes arma la tabla de direcciones (el "menu" del restaurante).
func (srv *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Publicas: no requieren token
	mux.HandleFunc("POST /auth/register", srv.handleRegister)
	mux.HandleFunc("POST /auth/login", srv.handleLogin)

	// Protegidas: RequireAuth las envuelve
	mux.HandleFunc("POST /files/put-init", RequireAuth(srv.handlePutInit))
	mux.HandleFunc("POST /files/put-complete", RequireAuth(srv.handlePutComplete))
	mux.HandleFunc("GET /files/{path...}", RequireAuth(srv.handleGetFile))
	mux.HandleFunc("GET /files", RequireAuth(srv.handleListFiles))
	mux.HandleFunc("DELETE /files/{path...}", RequireAuth(srv.handleDeleteFile))
	mux.HandleFunc("POST /directories", RequireAuth(srv.handleCreateDirectory))
	mux.HandleFunc("GET /directories/exists", RequireAuth(srv.handleDirectoryExists))
	mux.HandleFunc("DELETE /directories", RequireAuth(srv.handleDeleteDirectory))
	mux.HandleFunc("POST /files/lock", RequireAuth(srv.handleLock))
	mux.HandleFunc("POST /files/lock/heartbeat", RequireAuth(srv.handleLockHeartbeat))
	mux.HandleFunc("POST /files/lock/unlock", RequireAuth(srv.handleUnlock))

	// Endpoint para checkear si el token actual del usuario aún sirve
	mux.HandleFunc("GET /auth/session", RequireAuth(srv.handleValidateSession))

	// Internas: requieren el secreto de clúster
	mux.HandleFunc("POST /internal/datanodes/register",
		RequireClusterSecret(srv.clusterSecret, srv.handleRegisterDataNode))
	mux.HandleFunc("POST /internal/datanodes/heartbeat",
		RequireClusterSecret(srv.clusterSecret, srv.handleDataNodeHeartbeat))
	mux.HandleFunc("POST /internal/blocks/report",
		RequireClusterSecret(srv.clusterSecret, srv.handleReportBlock))

	// Liveness responde 200 para todo proceso vivo; Compose puede arrancar
	// DataNodes incluso si este MetaNode está en standby.
	mux.HandleFunc("GET /health/live", srv.handleLiveness)
	// Readiness exclusiva del líder: HAProxy solo envía tráfico de escritura aquí.
	mux.HandleFunc("GET /health/leader", srv.handleHealth)
	// Compatibilidad: /health sigue indicando si este proceso es líder.
	mux.HandleFunc("GET /health", srv.handleHealth)

	return logRequests(mux)
}

func (srv *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !srv.leader() {
		writeError(w, http.StatusServiceUnavailable, "i am not leader")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "leader"})
}

func (srv *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	role := "standby"
	if srv.leader() {
		role = "leader"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "role": role})
}

// logRequests records every HTTP request handled by the MetaNode without logging credentials.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		log.Printf("MetaNode HTTP request: remote=%s method=%s path=%s status=%d duration=%s",
			r.RemoteAddr, r.Method, r.URL.RequestURI(), wrapped.status, time.Since(started).Round(time.Millisecond))
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *responseRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
