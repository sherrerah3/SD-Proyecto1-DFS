package rest

import (
	"net/http"

	"dfsha/internal/server/metanode/service"
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // segundos
}

func (srv *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := service.Register(srv.store, req.Username, req.Password); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204: hecho, sin cuerpo
}

func (srv *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, err := service.Login(srv.store, req.Username, req.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresIn: int(service.SessionTTL.Seconds()),
	})
}
