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

// Valores definidos en la tabla de parametros del equipo
const (
	HeartbeatInterval = 5 * time.Second  // cada cuanto late un DataNode
	DeadThreshold     = 15 * time.Second // 5s x 3 reintentos: pasado esto, se da por muerto
)

var (
	ErrInvalidDataNode  = errors.New("datanode invalido: id y direccion host:puerto son obligatorios")
	ErrUnknownDataNode  = errors.New("datanode no registrado")
	ErrInvalidFreeSpace = errors.New("el espacio libre no puede ser negativo")
)

// RegisterDataNode anota un DataNode nuevo (o re-registra uno que se reinicio).
func RegisterDataNode(s store.Store, id, address string) error {
	if id == "" || len(id) > 64 {
		return ErrInvalidDataNode
	}
	// SplitHostPort comprueba que la direccion tenga forma "host:puerto"
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return ErrInvalidDataNode
	}

	return s.RegisterDataNode(store.DataNodeInfo{
		ID:            id,
		Address:       address,
		LastHeartbeat: time.Now(), // registrarse cuenta como "dar señal de vida"
		FreeBytes:     0,          // se actualiza con el primer heartbeat
		Alive:         true,
	})
}

// ReceiveHeartbeat procesa un latido: actualiza la hora y el espacio libre.
func ReceiveHeartbeat(s store.Store, id string, freeBytes int64) error {
	if freeBytes < 0 {
		return ErrInvalidFreeSpace
	}
	if err := s.UpdateHeartbeat(id, freeBytes); err != nil {
		if errors.Is(err, store.ErrDataNodeNotFound) {
			return ErrUnknownDataNode
		}
		return fmt.Errorf("error al registrar heartbeat: %w", err)
	}
	return nil
}

// CheckDataNodes marca muertos los nodos sin latir hace mas de threshold y devuelve sus ids.
func CheckDataNodes(s store.Store, now time.Time, threshold time.Duration) ([]string, error) {
	nodes, err := s.ListDataNodes()
	if err != nil {
		return nil, fmt.Errorf("error al listar datanodes: %w", err)
	}

	var dead []string
	for _, n := range nodes {
		// Solo se tocan los que figuran vivos y ya se pasaron del plazo.
		if n.Alive && now.Sub(n.LastHeartbeat) > threshold {
			err := s.SetDataNodeAlive(n.ID, false)
			if errors.Is(err, store.ErrDataNodeNotFound) {
				continue // desaparecio entre el listado y ahora; no pasa nada
			}
			if err != nil {
				return dead, fmt.Errorf("error al marcar %s como muerto: %w", n.ID, err)
			}
			dead = append(dead, n.ID)
		}
	}
	return dead, nil
}

// StartHealthMonitor corre la revision periodica en segundo plano hasta que se cancela el contexto.
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
					log.Printf("monitor de datanodes: %v", err)
					continue
				}
				for _, id := range dead {
					log.Printf("datanode %s marcado como muerto (sin heartbeat en %s)", id, threshold)
				}
			}
		}
	}()
}
