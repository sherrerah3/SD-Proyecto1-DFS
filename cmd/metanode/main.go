package main

import (
	"context"
	"errors"
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
	// The service cannot start without signing secrets.
	if err := service.RequireSecrets(); err != nil {
		return err
	}

	port := envOr("METANODE_PORT", "8080")
	nodeID := envOr("METANODE_ID", "MetaNode-1")
	sharedDir := os.Getenv("METANODE_SHARED_DIR")
	clusterSecret := os.Getenv("DFSHA_CLUSTER_SECRET")
	if clusterSecret == "" {
		return fmt.Errorf("missing environment variable DFSHA_CLUSTER_SECRET")
	}
	if v := os.Getenv("METANODE_BLOCK_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			service.SetBlockSize(n)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// /health reads this flag to determine whether this node is leader.
	var leader atomic.Bool
	var st store.Store

	statePath := os.Getenv("METANODE_STATE_PATH")
	if statePath == "" {
		if sharedDir != "" {
			statePath = filepath.Join(sharedDir, "state.json")
		} else {
			dataDir := envOr("METANODE_DATA_DIR", filepath.Join("data", "MetaNode", nodeID))
			statePath = filepath.Join(dataDir, "state.json")
		}
	}
	statePath, err := filepath.Abs(statePath)
	if err != nil {
		return fmt.Errorf("failed to resolve persistent state path: %w", err)
	}
	_, statErr := os.Stat(statePath)
	stateExisted := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("failed to inspect persistent state at %s: %w", statePath, statErr)
	}

	persistent, err := store.NewPersistentStore(statePath)
	if err != nil {
		return fmt.Errorf("failed to open persistent state at %s: %w", statePath, err)
	}
	st = persistent

	if stateExisted {
		log.Printf("MetaNode %s recovered persistent state from %s", nodeID, statePath)
	} else {
		log.Printf("MetaNode %s found no previous state; a new persistent state will be created at %s", nodeID, statePath)
	}

	if sharedDir == "" {
		// In single-node mode, this process is the only writer.
		leader.Store(true)
		persistent.SetCanWrite(func() bool { return leader.Load() })
		if err := persistent.Sync(); err != nil {
			return fmt.Errorf("failed to initialize persistent state at %s: %w", statePath, err)
		}
		log.Printf("MetaNode %s persistent state ready: path=%s users=%d", nodeID, statePath, persistent.CountUsers())
		log.Printf("MetaNode %s running in single-node mode (high availability disabled; persistent state enabled)", nodeID)
	} else {
		lockTTL := durationEnv("METANODE_LOCK_TTL", 15*time.Second)
		service.DefaultLockTTL = lockTTL

		// Fencing: only the node holding the current lock may write.
		persistent.SetCanWrite(func() bool {
			return service.OwnsLock(sharedDir, nodeID)
		})

		log.Printf("MetaNode %s starting in high-availability mode (shared_dir=%s, state_path=%s, initial_role=standby)", nodeID, sharedDir, statePath)
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

	log.Printf("MetaNode %s listening on :%s", nodeID, port)
	return httpSrv.ListenAndServe()
}

// leaderLoop monitors the current leader and acquires the lock after a failure.
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
			// Leader: confirm that this node still owns the lock.
			if !service.OwnsLock(sharedDir, nodeID) {
				log.Printf("MetaNode %s lost leadership", nodeID)
				leader.Store(false)
				if hbCancel != nil {
					hbCancel()
					hbCancel = nil
				}
			}
		} else {
			// Standby: take over if the current leader has failed.
			if !service.IsLeaderAlive(sharedDir, lockTTL) && service.TryAcquireLock(sharedDir, nodeID, lockTTL) {
				if err := st.Load(); err != nil {
					log.Printf("MetaNode %s failed to reload state: %v", nodeID, err)
				} else if err := st.Sync(); err != nil {
					log.Printf("MetaNode %s failed to initialize persistent state after leadership election: %v", nodeID, err)
				} else {
					log.Printf("MetaNode %s became leader with persistent state loaded (users=%d)", nodeID, st.CountUsers())
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
