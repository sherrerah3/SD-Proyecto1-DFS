package rest

import (
	"log"
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
		log.Printf("MetaNode: user registration failed username=%q remote=%s error=%v", req.Username, r.RemoteAddr, err)
		writeServiceError(w, err)
		return
	}
	log.Printf("MetaNode: user registered username=%q remote=%s", req.Username, r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent) // 204: hecho, sin cuerpo
}

func (srv *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token, err := service.Login(srv.store, req.Username, req.Password)
	if err != nil {
		log.Printf("MetaNode: user login failed username=%q remote=%s error=%v", req.Username, r.RemoteAddr, err)
		writeServiceError(w, err)
		return
	}
	log.Printf("MetaNode: user logged in username=%q remote=%s", req.Username, r.RemoteAddr)
	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresIn: int(service.SessionTTL.Seconds()),
	})
}

func (srv *Server) handleValidateSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"username": usernameFrom(r),
	})
}
