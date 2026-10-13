package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"dfsha/internal/server/metanode/api/rest"
	"dfsha/internal/server/metanode/service"
	"dfsha/internal/server/metanode/store"
)

func main() {
	port := os.Getenv("METANODE_PORT")
	if port == "" {
		port = "8080"
	}

	ctx := context.Background()
	st := store.NewMemoryStore()

	service.StartHealthMonitor(ctx, st, service.HeartbeatInterval, service.DeadThreshold)

	if os.Getenv("METANODE_DEV_SEED") == "1" {
		seedDevDataNodes(ctx, st)
	}

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           rest.NewServer(st).Routes(),
		ReadHeaderTimeout: 5 * time.Second, // evita conexiones que se quedan "colgadas" sin enviar nada
	}
	log.Printf("metanode escuchando en :%s", port)
	log.Fatal(httpSrv.ListenAndServe())
}

// TEMPORAL: finge 3 DataNodes con latidos para probar put-init; borrar con el DataNode real.
func seedDevDataNodes(ctx context.Context, st store.Store) {
	ids := []string{"dn1", "dn2", "dn3"}
	for i, id := range ids {
		if err := service.RegisterDataNode(st, id, fmt.Sprintf("localhost:%d", 9091+i)); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}
	go func() {
		t := time.NewTicker(service.HeartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, id := range ids {
					_ = service.ReceiveHeartbeat(st, id, 10<<30)
				}
			}
		}
	}()
}
