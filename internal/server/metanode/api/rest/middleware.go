package rest

import (
	"context"
	"net/http"
	"strings"

	"dfsha/internal/server/metanode/service"
)

type ctxKey string

const usernameKey ctxKey = "username"

// clusterSecretHeader lleva el secreto entre nodos.
const clusterSecretHeader = "X-Cluster-Secret"

// RequireClusterSecret protege las rutas /internal/*.
func RequireClusterSecret(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret != "" && subtleCompare(r.Header.Get(clusterSecretHeader), secret) {
			next(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "secreto de clúster inválido")
	}
}

// subtleCompare compara dos cadenas en tiempo constante.
func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// RequireAuth exige un JWT de sesion valido y guarda el username en el contexto.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "falta el header Authorization: Bearer <token>")
			return
		}

		username, err := service.ValidateJWT(token)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		ctx := context.WithValue(r.Context(), usernameKey, username)
		next(w, r.WithContext(ctx))
	}
}

// usernameFrom recupera el usuario que RequireAuth dejo en el contexto.
func usernameFrom(r *http.Request) string {
	u, _ := r.Context().Value(usernameKey).(string)
	return u
}
