package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"dfsha/internal/server/metanode/store"
)

// Values defined in the team's parameter table
const (
	HeartbeatInterval = 5 * time.Second  // how often a DataNode sends a heartbeat
	DeadThreshold     = 15 * time.Second // 5s x 3 retries: after this, the node is considered dead
)

var (
	ErrInvalidDataNode  = errors.New("invalid datanode: id and host:port address are required")
	ErrUnknownDataNode  = errors.New("datanode not registered")
	ErrInvalidFreeSpace = errors.New("free space cannot be negative")
)

// RegisterDataNode registers a new DataNode (or re-registers one that has restarted).
func RegisterDataNode(s store.Store, id, address string) error {
	if id == "" || len(id) > 64 {
		return ErrInvalidDataNode
	}
	// SplitHostPort checks that the address has the "host:port" format.
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return ErrInvalidDataNode
	}

	return s.RegisterDataNode(store.DataNodeInfo{
		ID:            id,
		Address:       address,
		LastHeartbeat: time.Now(), // registration counts as a "heartbeat"
		FreeBytes:     0,          // updated with the first heartbeat
		Alive:         true,
	})
}

// ReceiveHeartbeat processes a heartbeat: it updates the timestamp and free space.
func ReceiveHeartbeat(s store.Store, id string, freeBytes int64) error {
	if freeBytes < 0 {
		return ErrInvalidFreeSpace
	}
	if err := s.UpdateHeartbeat(id, freeBytes); err != nil {
		if errors.Is(err, store.ErrDataNodeNotFound) {
			return ErrUnknownDataNode
		}
		return fmt.Errorf("error recording heartbeat: %w", err)
	}
	return nil
}

// CheckDataNodes marks nodes that have not sent a heartbeat for longer than the threshold as dead and returns their IDs.
func CheckDataNodes(s store.Store, now time.Time, threshold time.Duration) ([]string, error) {
	nodes, err := s.ListDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error listing datanodes: %w", err)
	}

	var dead []string
	for _, n := range nodes {
		// Only process nodes that are marked alive and have exceeded the timeout.
		if n.Alive && now.Sub(n.LastHeartbeat) > threshold {
			err := s.SetDataNodeAlive(n.ID, false)
			if errors.Is(err, store.ErrDataNodeNotFound) {
				continue // the node disappeared between listing and now; nothing to do
			}
			if err != nil {
				return dead, fmt.Errorf("error marking %s as dead: %w", n.ID, err)
			}
			dead = append(dead, n.ID)
		}
	}
	return dead, nil
}

// StartHealthMonitor runs periodic checks in the background until the context is canceled.
func StartHealthMonitor(ctx context.Context, s store.Store, interval, threshold time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				dead, err := CheckDataNodes(s, now, threshold)
				if err != nil {
					log.Printf("datanode monitor: %v", err)
					continue
				}
				for _, id := range dead {
					log.Printf("datanode %s marked as dead (no heartbeat for %s)", id, threshold)
				}
			}
		}
	}()
}
