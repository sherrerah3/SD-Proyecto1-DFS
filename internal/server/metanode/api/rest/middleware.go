package rest

import (
	"context"
	"net/http"
	"strings"

	"dfsha/internal/server/metanode/service"
)

type ctxKey string

const usernameKey ctxKey = "username"

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
