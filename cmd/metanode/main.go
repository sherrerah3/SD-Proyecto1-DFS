package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"dfsha/internal/server/metanode/api/rest"
	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Sin claves de firma el servicio no arranca.
	if err := service.RequireSecrets(); err != nil {
		return err
	}

	port := envOr("METANODE_PORT", "8080")
	nodeID := envOr("METANODE_ID", "metanode-1")
	sharedDir := os.Getenv("METANODE_SHARED_DIR")
	clusterSecret := os.Getenv("DFSHA_CLUSTER_SECRET")
	if clusterSecret == "" {
		return fmt.Errorf("falta la variable de entorno DFSHA_CLUSTER_SECRET")
	}
	if v := os.Getenv("METANODE_BLOCK_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			service.SetBlockSize(n)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Lo consulta /health.
	var leader atomic.Bool

	var st store.Store
	if sharedDir == "" {
		// Nodo único: memoria y siempre líder.
		st = store.NewMemoryStore()
		leader.Store(true)
		log.Printf("metanode %s en modo nodo único (sin alta disponibilidad)", nodeID)
	} else {
		lockTTL := durationEnv("METANODE_LOCK_TTL", 15*time.Second)
		service.DefaultLockTTL = lockTTL

		persistent, err := store.NewPersistentStore(filepath.Join(sharedDir, "state.json"))
		if err != nil {
			return fmt.Errorf("no se pudo abrir el estado persistente: %w", err)
		}
		// Fencing: solo escribe quien tiene el lock vigente.
		persistent.SetCanWrite(func() bool {
			return service.OwnsLock(sharedDir, nodeID)
		})
		st = persistent

		// Elección de líder.
		go leaderLoop(ctx, persistent, sharedDir, nodeID, lockTTL, &leader)
	}

	service.StartHealthMonitor(ctx, st, service.HeartbeatInterval, service.DeadThreshold)

	srv := rest.NewServer(st)
	srv.SetClusterSecret(clusterSecret)
	srv.SetLeaderCheck(leader.Load)

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("metanode %s escuchando en :%s", nodeID, port)
	return httpSrv.ListenAndServe()
}

// leaderLoop vigila al líder y toma el lock si cae.
func leaderLoop(
	ctx context.Context,
	st *store.PersistentStore,
	sharedDir, nodeID string,
	lockTTL time.Duration,
	leader *atomic.Bool,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var hbCancel context.CancelFunc

	for {
		if leader.Load() {
			// Líder: confirmar que seguimos con el lock.
			if !service.OwnsLock(sharedDir, nodeID) {
				log.Printf("metanode %s perdió el liderazgo", nodeID)
				leader.Store(false)
				if hbCancel != nil {
					hbCancel()
					hbCancel = nil
				}
			}
		} else {
			// Respaldo: tomar el control si el líder murió.
			if !service.IsLeaderAlive(sharedDir, lockTTL) && service.TryAcquireLock(sharedDir, nodeID, lockTTL) {
				if err := st.Load(); err != nil {
					log.Printf("metanode %s no pudo recargar el estado: %v", nodeID, err)
				} else {
					log.Printf("metanode %s asumió como líder", nodeID)
					leader.Store(true)
					hbCtx, cancel := context.WithCancel(ctx)
					hbCancel = cancel
					service.StartHeartbeatWriter(hbCtx, sharedDir, nodeID, service.HeartbeatInterval)
				}
			}
		}

		select {
		case <-ctx.Done():
			if hbCancel != nil {
				hbCancel()
			}
			return
		case <-ticker.C:
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
