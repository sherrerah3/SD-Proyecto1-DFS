package datanode

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	datanodev1 "dfsha/internal/proto/datanode/v1"
	"dfsha/internal/server/datanode/auth"
	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/datanode/crypto"
	"dfsha/internal/server/datanode/grpcserver"
	"dfsha/internal/server/datanode/heartbeat"
	"dfsha/internal/server/datanode/replication"
)

// Run levanta el DataNode: arma el store, el servidor gRPC y escucha conexiones.
func Run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	store, err := buildStore(cfg)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", cfg.Addr, err)
	}

	metanodeURL := os.Getenv("DFSHA_METANODE_URL")
	if metanodeURL == "" {
		metanodeURL = "http://localhost:8080"
	}

	advertiseAddr := os.Getenv("DFSHA_DATANODE_ADVERTISE_ADDR")
	if advertiseAddr == "" {
		_, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return fmt.Errorf("invalid gRPC address %q: %w", cfg.Addr, err)
		}
		advertiseAddr = net.JoinHostPort("localhost", port)
	}

	// Reporter hacia el Metanode.
	reporter := heartbeat.NewHTTPReporter(metanodeURL)
	reporter.SetClusterSecret(cfg.ClusterSecret)

	// Replicador entre DataNodes.
	replicator := replication.NewReplicator()
	replicator.SetClusterSecret(cfg.ClusterSecret)

	// Servidor gRPC con auth y reporte de bloques.
	validator := auth.NewValidator(cfg.CapabilitySecret)
	grpcSrv := grpcserver.NewServer(store, replicator).
		WithAuth(validator, cfg.ClusterSecret, cfg.ID).
		WithReporter(reporter)

	server := grpc.NewServer(
		grpc.StatsHandler(grpcserver.NewLoggingStatsHandler(cfg.ID)),
		grpc.UnaryInterceptor(grpcserver.UnaryLoggingInterceptor(cfg.ID)),
		grpc.StreamInterceptor(grpcserver.StreamLoggingInterceptor(cfg.ID)),
	)
	datanodev1.RegisterBlockServiceServer(server, grpcSrv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := heartbeat.NewAgent(
		cfg.ID,
		advertiseAddr,
		store,
		reporter,
		func() int64 { return freeBytes(cfg.Dir) },
	)
	agent.SetDeleter(store) // aplica los borrados que ordena el Metanode

	go func() {
		if err := agent.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("datanode %s: heartbeat agent exited with error: %v", cfg.ID, err)
		}
	}()

	// Apagado ordenado.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Printf("datanode %s: shutting down...", cfg.ID)
		cancel()
		server.GracefulStop()
	}()

	log.Printf("datanode %s listening on %s (dir=%s, encryption=%t)", cfg.ID, cfg.Addr, cfg.Dir, cfg.Key != nil)

	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("gRPC server exited with error: %w", err)
	}

	return nil
}

// buildStore crea el store en disco y, si hay llave, lo envuelve con cifrado.
func buildStore(cfg Config) (blockstore.BlockStore, error) {
	disk, err := blockstore.NewDiskStore(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk store: %w", err)
	}

	if cfg.Key == nil {
		return disk, nil
	}

	cipher, err := crypto.NewAESGCM(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	return blockstore.NewEncryptedBlockStore(disk, cipher), nil
}
