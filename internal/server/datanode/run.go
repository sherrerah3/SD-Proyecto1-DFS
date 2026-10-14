package datanode

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	datanodev1 "dfsha/internal/proto/datanode/v1"
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
		return fmt.Errorf("configuración inválida: %w", err)
	}

	store, err := buildStore(cfg)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("no se pudo escuchar en %s: %w", cfg.Addr, err)
	}

	server := grpc.NewServer()
	datanodev1.RegisterBlockServiceServer(server, grpcserver.NewServer(store, replication.NewReplicator()))

	// El heartbeat corre en paralelo al servidor. Usa el LogReporter por defecto
	// hasta que el Metanode (Persona 2) publique el servicio gRPC de Heartbeat.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metanodeURL := os.Getenv("DFSHA_METANODE_URL")
	if metanodeURL == "" {
		metanodeURL = "http://localhost:8080"
	}

	advertiseAddr := os.Getenv("DFSHA_DATANODE_ADVERTISE_ADDR")
	if advertiseAddr == "" {
		_, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return fmt.Errorf("dirección gRPC inválida %q: %w", cfg.Addr, err)
		}

		advertiseAddr = net.JoinHostPort("localhost", port)
	}

	agent := heartbeat.NewAgent(
		cfg.ID,
		advertiseAddr,
		store,
		heartbeat.NewHTTPReporter(metanodeURL),
		nil,
	)

	go func() {
		if err := agent.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("el agente de heartbeat terminó con error: %v", err)
		}
	}()

	log.Printf("datanode %s escuchando en %s (dir=%s, cifrado=%t)", cfg.ID, cfg.Addr, cfg.Dir, cfg.Key != nil)

	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("el servidor gRPC terminó con error: %w", err)
	}

	return nil
}

// buildStore crea el store en disco y, si hay llave, lo envuelve con cifrado.
func buildStore(cfg Config) (blockstore.BlockStore, error) {
	disk, err := blockstore.NewDiskStore(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el store en disco: %w", err)
	}

	if cfg.Key == nil {
		return disk, nil
	}

	cipher, err := crypto.NewAESGCM(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el cifrador: %w", err)
	}

	return blockstore.NewEncryptedBlockStore(disk, cipher), nil
}
