package grpcserver

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
)

// UnaryLoggingInterceptor logs every incoming unary gRPC call and its result.
func UnaryLoggingInterceptor(nodeID string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		remote := remoteAddress(ctx)
		log.Printf("datanode %s: gRPC request started method=%s remote=%s", nodeID, info.FullMethod, remote)
		resp, err := handler(ctx, req)
		if err != nil {
			log.Printf("datanode %s: gRPC request failed method=%s remote=%s duration=%s error=%v", nodeID, info.FullMethod, remote, time.Since(started).Round(time.Millisecond), err)
		} else {
			log.Printf("datanode %s: gRPC request completed method=%s remote=%s duration=%s", nodeID, info.FullMethod, remote, time.Since(started).Round(time.Millisecond))
		}
		return resp, err
	}
}

// StreamLoggingInterceptor logs every incoming streaming gRPC call and its result.
func StreamLoggingInterceptor(nodeID string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		started := time.Now()
		remote := remoteAddress(stream.Context())
		log.Printf("datanode %s: gRPC stream started method=%s remote=%s", nodeID, info.FullMethod, remote)
		err := handler(srv, stream)
		if err != nil {
			log.Printf("datanode %s: gRPC stream failed method=%s remote=%s duration=%s error=%v", nodeID, info.FullMethod, remote, time.Since(started).Round(time.Millisecond), err)
		} else {
			log.Printf("datanode %s: gRPC stream completed method=%s remote=%s duration=%s", nodeID, info.FullMethod, remote, time.Since(started).Round(time.Millisecond))
		}
		return err
	}
}

func remoteAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

// NewLoggingStatsHandler logs gRPC transport connections as clients connect and disconnect.
func NewLoggingStatsHandler(nodeID string) stats.Handler {
	return &loggingStatsHandler{nodeID: nodeID}
}

type connectionContextKey struct{}

type loggingStatsHandler struct {
	nodeID string
}

func (h *loggingStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *loggingStatsHandler) HandleRPC(context.Context, stats.RPCStats) {}

func (h *loggingStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	remote := "unknown"
	if info != nil && info.RemoteAddr != nil {
		remote = info.RemoteAddr.String()
	}
	return context.WithValue(ctx, connectionContextKey{}, remote)
}

func (h *loggingStatsHandler) HandleConn(ctx context.Context, stat stats.ConnStats) {
	remote, _ := ctx.Value(connectionContextKey{}).(string)
	switch stat.(type) {
	case *stats.ConnBegin:
		log.Printf("datanode %s: gRPC client connected remote=%s", h.nodeID, remote)
	case *stats.ConnEnd:
		log.Printf("datanode %s: gRPC client disconnected remote=%s", h.nodeID, remote)
	}
}
