package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Nombres de los archivos de coordinación dentro de la carpeta compartida.
const (
	heartbeatFileName = "heartbeat.txt"
	lockFileName      = "metanode.lock"
)

// lockInfo es el contenido del archivo de lock: quién lo tiene y desde cuándo.
type lockInfo struct {
	Owner     string    `json:"owner"`
	Timestamp time.Time `json:"timestamp"`
}

// StartHeartbeatWriter escribe la hora actual cada interval mientras el nodo sea líder.
func StartHeartbeatWriter(ctx context.Context, basePath, nodeID string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			if err := writeHeartbeat(basePath); err != nil {
				// No es fatal: se reintenta en el siguiente tick.
				_ = err
			}
			if err := RenewLock(basePath, nodeID); err != nil {
				_ = err
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// writeHeartbeat guarda la hora actual en heartbeat.txt de forma atómica.
func writeHeartbeat(basePath string) error {
	path := filepath.Join(basePath, heartbeatFileName)
	data := []byte(time.Now().UTC().Format(time.RFC3339Nano))
	return atomicWrite(path, data)
}

// IsLeaderAlive devuelve true si el heartbeat del líder es más reciente que deadThreshold.
func IsLeaderAlive(basePath string, deadThreshold time.Duration) bool {
	data, err := os.ReadFile(filepath.Join(basePath, heartbeatFileName))
	if err != nil {
		return false // sin heartbeat no hay líder vivo
	}

	last, err := time.Parse(time.RFC3339Nano, string(data))
	if err != nil {
		return false
	}

	return time.Since(last) <= deadThreshold
}

// TryAcquireLock toma el lock si está libre o vencido; devuelve true si lo consiguió.
func TryAcquireLock(basePath, nodeID string, lockTTL time.Duration) bool {
	path := filepath.Join(basePath, lockFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		var current lockInfo
		if json.Unmarshal(data, &current) == nil {
			// Lock reciente de OTRO nodo: no se puede tomar.
			if current.Owner != nodeID && time.Since(current.Timestamp) < lockTTL {
				return false
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false // error de lectura distinto de "no existe"
	}

	return writeLock(path, nodeID) == nil
}

// RenewLock reescribe el lock con el nodo actual y la hora de ahora.
func RenewLock(basePath, nodeID string) error {
	return writeLock(filepath.Join(basePath, lockFileName), nodeID)
}

// OwnsLock indica si el lock pertenece ahora mismo a nodeID (lo usa canWrite del store).
func OwnsLock(basePath, nodeID string) bool {
	data, err := os.ReadFile(filepath.Join(basePath, lockFileName))
	if err != nil {
		return false
	}
	var current lockInfo
	if json.Unmarshal(data, &current) != nil {
		return false
	}
	return current.Owner == nodeID
}

func writeLock(path, nodeID string) error {
	data, err := json.Marshal(lockInfo{Owner: nodeID, Timestamp: time.Now().UTC()})
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// atomicWrite escribe a un temporal y renombra, para no dejar archivos a medias.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
