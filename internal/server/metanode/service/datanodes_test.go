package service

import (
	"testing"
	"time"

	"dfsha/internal/server/metanode/store"
)

func TestCheckDataNodesMarcaMuerto(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	// A los 20s sin latir supera el umbral de 15s: queda muerto.
	dead, err := CheckDataNodes(s, time.Now().Add(20*time.Second), DeadThreshold)
	if err != nil {
		t.Fatalf("CheckDataNodes falló: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("se esperaba 1 nodo muerto, hay %d", len(dead))
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 0 {
		t.Fatalf("un nodo muerto no debe aparecer entre los vivos, hay %d", len(alive))
	}
}

func TestCheckDataNodesSigueVivo(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	// A los 10s aún no supera el umbral de 15s: sigue vivo.
	dead, err := CheckDataNodes(s, time.Now().Add(10*time.Second), DeadThreshold)
	if err != nil {
		t.Fatalf("CheckDataNodes falló: %v", err)
	}
	if len(dead) != 0 {
		t.Fatalf("ningún nodo debía morir, murieron %d", len(dead))
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 1 {
		t.Fatalf("el nodo debía seguir vivo, hay %d", len(alive))
	}
}

func TestHeartbeatRevivePorNodoMuerto(t *testing.T) {
	s := store.NewMemoryStore()
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	// Se marca muerto por inactividad.
	if _, err := CheckDataNodes(s, time.Now().Add(20*time.Second), DeadThreshold); err != nil {
		t.Fatalf("preparación falló: %v", err)
	}
	if alive, _ := s.ListAliveDataNodes(); len(alive) != 0 {
		t.Fatalf("el nodo debía estar muerto antes del heartbeat")
	}

	// Un heartbeat lo revive.
	if err := ReceiveHeartbeat(s, "dn-1", 100); err != nil {
		t.Fatalf("heartbeat falló: %v", err)
	}

	alive, _ := s.ListAliveDataNodes()
	if len(alive) != 1 {
		t.Fatalf("el nodo debía revivir tras el heartbeat, hay %d vivos", len(alive))
	}
}

func TestHeartbeatNodoDesconocido(t *testing.T) {
	s := store.NewMemoryStore()

	if err := ReceiveHeartbeat(s, "nunca-registrado", 100); err != ErrUnknownDataNode {
		t.Fatalf("error = %v, se esperaba ErrUnknownDataNode", err)
	}
}

func TestRegisterDataNodeDireccionInvalida(t *testing.T) {
	s := store.NewMemoryStore()

	direcciones := map[string]string{
		"vacía":      "",
		"sin puerto": "localhost",
		"sin host":   ":9090",
	}

	for nombre, addr := range direcciones {
		t.Run(nombre, func(t *testing.T) {
			if err := RegisterDataNode(s, "dn-1", addr); err != ErrInvalidDataNode {
				t.Fatalf("error = %v, se esperaba ErrInvalidDataNode", err)
			}
		})
	}
}

func TestRegisterDataNodeSobrescribe(t *testing.T) {
	s := store.NewMemoryStore()

	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("primer registro falló: %v", err)
	}
	if err := RegisterDataNode(s, "dn-1", "127.0.0.1:9999"); err != nil {
		t.Fatalf("segundo registro falló: %v", err)
	}

	nodes, _ := s.ListAliveDataNodes()
	if len(nodes) != 1 {
		t.Fatalf("debía quedar una sola ficha, hay %d", len(nodes))
	}
	if nodes[0].Address != "127.0.0.1:9999" {
		t.Fatalf("la dirección no se actualizó: %q", nodes[0].Address)
	}
}
