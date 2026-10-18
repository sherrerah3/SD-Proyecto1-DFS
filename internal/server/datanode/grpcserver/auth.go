package grpcserver

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"dfsha/internal/server/datanode/auth"
)

const (
	mdAuthorization = "authorization"
	mdClusterSecret = "x-cluster-secret"
)

// capabilityFromContext valida el token de la metadata (sin validador no verifica, solo tests)
func (s *Server) capabilityFromContext(ctx context.Context, op string) (auth.Capability, error) {
	if s.validator == nil {
		return auth.Capability{}, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return auth.Capability{}, status.Error(codes.Unauthenticated, "falta la metadata de autorización")
	}

	token := ""
	if vals := md.Get(mdAuthorization); len(vals) > 0 {
		token = strings.TrimPrefix(vals[0], "Bearer ")
	}
	if token == "" {
		return auth.Capability{}, status.Error(codes.Unauthenticated, "falta el capability token")
	}

	cap, err := s.validator.Validate(token, op)
	if err != nil {
		return auth.Capability{}, status.Error(codes.PermissionDenied, "capability token inválido")
	}
	return cap, nil
}

// authorizeBlock verifica que la capability autorice el block_id concreto.
func (s *Server) authorizeBlock(cap auth.Capability, blockID string) error {
	if s.validator == nil {
		return nil
	}
	if !cap.Allows(blockID) {
		return status.Error(codes.PermissionDenied, "el token no autoriza este bloque")
	}
	return nil
}

// requireClusterSecret autentica llamadas entre nodos
func (s *Server) requireClusterSecret(ctx context.Context) error {
	if s.clusterSecret == "" {
		return nil // sin secreto configurado: solo pruebas
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "falta la metadata del clúster")
	}
	got := ""
	if vals := md.Get(mdClusterSecret); len(vals) > 0 {
		got = vals[0]
	}
	if subtleEqual(got, s.clusterSecret) {
		return nil
	}
	return status.Error(codes.PermissionDenied, "secreto de clúster inválido")
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
