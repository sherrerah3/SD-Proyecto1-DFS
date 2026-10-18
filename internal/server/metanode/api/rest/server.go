package rest

import (
	"net/http"

	"dfsha/internal/server/metanode/store"
)

type Server struct {
	store         store.Store
	clusterSecret string
	isLeader      func() bool // nil = siempre líder
}

func NewServer(s store.Store) *Server {
	return &Server{store: s}
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

	// Endpoint para checkear si el token actual del usuario aún sirve
	mux.HandleFunc("GET /auth/session", RequireAuth(srv.handleValidateSession))

	// Internas: requieren el secreto de clúster
	mux.HandleFunc("POST /internal/datanodes/register",
		RequireClusterSecret(srv.clusterSecret, srv.handleRegisterDataNode))
	mux.HandleFunc("POST /internal/datanodes/heartbeat",
		RequireClusterSecret(srv.clusterSecret, srv.handleDataNodeHeartbeat))
	mux.HandleFunc("POST /internal/blocks/report",
		RequireClusterSecret(srv.clusterSecret, srv.handleReportBlock))

	// 200 solo en el líder (lo usa el proxy)
	mux.HandleFunc("GET /health", srv.handleHealth)

	return mux
}

func (srv *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !srv.leader() {
		writeError(w, http.StatusServiceUnavailable, "no soy el líder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "leader"})
}
