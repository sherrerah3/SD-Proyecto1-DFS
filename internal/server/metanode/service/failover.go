package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// StartHeartbeatWriter renueva el lock y escribe el heartbeat mientras seamos dueños.
func StartHeartbeatWriter(ctx context.Context, basePath, nodeID string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			// Si no renueva, ya no somos líderes: sin heartbeat.
			if err := RenewLock(basePath, nodeID); err == nil {
				_ = writeHeartbeat(basePath)
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

// DefaultLockTTL es el tiempo tras el cual un lock sin renovar vence.
var DefaultLockTTL = 15 * time.Second

// TryAcquireLock toma el lock: libre (O_EXCL) o vencido (rename y O_EXCL).
func TryAcquireLock(basePath, nodeID string, lockTTL time.Duration) bool {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return false
	}
	path := filepath.Join(basePath, lockFileName)

	// Ya es nuestro: renovar.
	if cur, err := readLock(path); err == nil && cur.Owner == nodeID {
		return RenewLock(basePath, nodeID) == nil
	}

	// Lock libre.
	if createLockExclusive(path, nodeID) == nil {
		return true
	}

	// Solo se roba si está vencido.
	cur, err := readLock(path)
	if err != nil {
		return false
	}
	if time.Since(cur.Timestamp) < lockTTL {
		return false // lock reciente de otro nodo
	}

	// Solo un nodo gana el rename.
	stale := fmt.Sprintf("%s.stale-%s-%d", path, nodeID, time.Now().UnixNano())
	if err := os.Rename(path, stale); err != nil {
		return false
	}
	_ = os.Remove(stale)

	return createLockExclusive(path, nodeID) == nil
}

// createLockExclusive crea el lock con O_EXCL.
func createLockExclusive(path, nodeID string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(lockInfo{Owner: nodeID, Timestamp: time.Now().UTC()})
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// RenewLock renueva el lock solo si sigue siendo nuestro.
func RenewLock(basePath, nodeID string) error {
	path := filepath.Join(basePath, lockFileName)
	cur, err := readLock(path)
	if err != nil {
		return err
	}
	if cur.Owner != nodeID {
		return errors.New("el lock ya no nos pertenece")
	}
	return writeLock(path, nodeID)
}

// OwnsLock indica si el lock es de nodeID y no está vencido (fencing).
func OwnsLock(basePath, nodeID string) bool {
	cur, err := readLock(filepath.Join(basePath, lockFileName))
	if err != nil {
		return false
	}
	if cur.Owner != nodeID {
		return false
	}
	return time.Since(cur.Timestamp) < DefaultLockTTL
}

func readLock(path string) (lockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockInfo{}, err
	}
	var li lockInfo
	if err := json.Unmarshal(data, &li); err != nil {
		return lockInfo{}, err
	}
	return li, nil
}

func writeLock(path, nodeID string) error {
	data, err := json.Marshal(lockInfo{Owner: nodeID, Timestamp: time.Now().UTC()})
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// atomicWrite escribe a un temporal con fsync y renombra.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
