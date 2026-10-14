package rest

import (
	"net/http"

	"dfsha/internal/server/metanode/store"
)

type Server struct {
	store store.Store
}

func NewServer(s store.Store) *Server {
	return &Server{store: s}
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

	// Endpoint para checkear si el token actual del usuario aún sirve
	mux.HandleFunc("GET /auth/session", RequireAuth(srv.handleValidateSession))

	// Registro y heartbeat de DataNodes.
	mux.HandleFunc("POST /internal/datanodes/register", srv.handleRegisterDataNode)
	mux.HandleFunc("POST /internal/datanodes/heartbeat", srv.handleDataNodeHeartbeat)

	return mux
}
