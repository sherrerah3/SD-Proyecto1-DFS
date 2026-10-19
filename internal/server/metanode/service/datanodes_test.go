package service

import (
	"testing"
	"time"

	"dfsha/internal/server/metanode/store"
)

func TestCheckDataNodesMarksDead(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// After 20s without a heartbeat, the 15s threshold is exceeded: the node is marked dead.
	dead, err := CheckDataNodes(s, time.Now().Add(20*time.Second), DeadThreshold)
	if err != nil {
		t.Fatalf("CheckDataNodes failed: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("expected 1 dead node, got %d", len(dead))
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 0 {
		t.Fatalf("a dead node must not appear among alive nodes, got %d", len(alive))
	}
}

func TestCheckDataNodesRemainsAlive(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// After 10s, the 15s threshold has not been exceeded: the node remains alive.
	dead, err := CheckDataNodes(s, time.Now().Add(10*time.Second), DeadThreshold)
	if err != nil {
		t.Fatalf("CheckDataNodes failed: %v", err)
	}
	if len(dead) != 0 {
		t.Fatalf("no nodes should have died, got %d dead nodes", len(dead))
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 1 {
		t.Fatalf("the node should remain alive, got %d", len(alive))
	}
}

func TestHeartbeatRevivesDeadNode(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Mark the node as dead due to inactivity.
	if _, err := CheckDataNodes(s, time.Now().Add(20*time.Second), DeadThreshold); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if alive, _ := s.ListAliveDataNodes(); len(alive) != 0 {
		t.Fatalf("the node should be dead before the heartbeat")
	}

	// A heartbeat revives the node.
	if err := ReceiveHeartbeat(s, "dn-1", 100); err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 1 {
		t.Fatalf("the node should revive after the heartbeat, got %d alive nodes", len(alive))
	}
}

func TestHeartbeatUnknownNode(t *testing.T) {
	s := store.NewMemoryStore()

	if err := ReceiveHeartbeat(s, "never-registered", 100); err != ErrUnknownDataNode {
		t.Fatalf("error = %v, expected ErrUnknownDataNode", err)
	}
}

func TestRegisterDataNodeInvalidAddress(t *testing.T) {
	s := store.NewMemoryStore()

	addresses := map[string]string{
		"empty":        "",
		"missing port": "localhost",
		"missing host": ":9090",
	}

	for name, addr := range addresses {
		t.Run(name, func(t *testing.T) {
			if err := RegisterDataNode(s, "dn-1", addr); err != ErrInvalidDataNode {
				t.Fatalf("error = %v, expected ErrInvalidDataNode", err)
			}
		})
	}
}

func TestRegisterDataNodeOverwrites(t *testing.T) {
	s := store.NewMemoryStore()

	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9999"); err != nil {
		t.Fatalf("second registration failed: %v", err)
	}

	nodes, _ := s.ListAliveDataNodes()
	if len(nodes) != 1 {
		t.Fatalf("there should be only one record, got %d", len(nodes))
	}
	if nodes[0].Address != "127.0.0.1:9999" {
		t.Fatalf("the address was not updated: %q", nodes[0].Address)
	}
}
