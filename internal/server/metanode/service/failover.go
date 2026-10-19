package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Nombres de los archivos de coordinación dentro de la carpeta compartida.
const (
	heartbeatFileName = "heartbeat.txt"
	lockFileName      = "MetaNode.lock"
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

type heldFileLock struct {
	file    *os.File
	release func() error
}

type heldLeaderLock struct {
	nodeID string
	lock   *heldFileLock
}

var (
	leaderLocksMu sync.Mutex
	leaderLocks   = make(map[string]*heldLeaderLock)
)

func lockPath(basePath string) (string, error) {
	abs, err := filepath.Abs(basePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, lockFileName), nil
}

// TryAcquireLock toma un bloqueo exclusivo que el sistema operativo libera incluso si
// el contenedor muere abruptamente. lockTTL se ignora deliberadamente: nunca se roba
// un lock solo por un heartbeat lento, porque eso permitiría dos líderes simultáneos.
func TryAcquireLock(basePath, nodeID string, _ time.Duration) bool {
	if nodeID == "" || os.MkdirAll(basePath, 0o755) != nil {
		return false
	}
	path, err := lockPath(basePath)
	if err != nil {
		return false
	}

	leaderLocksMu.Lock()
	defer leaderLocksMu.Unlock()
	if held, ok := leaderLocks[path]; ok {
		if held.nodeID != nodeID {
			return false
		}
		return writeHeldLock(held) == nil
	}

	lock, acquired, err := tryPlatformFileLock(path)
	if err != nil || !acquired {
		return false
	}
	held := &heldLeaderLock{nodeID: nodeID, lock: lock}
	if err := writeHeldLock(held); err != nil {
		_ = lock.release()
		return false
	}
	leaderLocks[path] = held
	return true
}

func writeHeldLock(held *heldLeaderLock) error {
	if held == nil || held.lock == nil || held.lock.file == nil {
		return errors.New("leader lock is not held by this process")
	}
	data, err := json.Marshal(lockInfo{Owner: held.nodeID, Timestamp: time.Now().UTC()})
	if err != nil {
		return err
	}
	f := held.lock.file
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// RenewLock actualiza el registro sin sustituir el archivo bloqueado (su inode/handle
// debe mantenerse para que el lock siga protegiendo a todos los procesos).
func RenewLock(basePath, nodeID string) error {
	path, err := lockPath(basePath)
	if err != nil {
		return err
	}
	leaderLocksMu.Lock()
	defer leaderLocksMu.Unlock()
	held, ok := leaderLocks[path]
	if !ok || held.nodeID != nodeID {
		return errors.New("leader lock is not owned by this node")
	}
	return writeHeldLock(held)
}

// OwnsLock se basa en el bloqueo exclusivo vivo del proceso; no en una marca de tiempo
// que podría ser vencida por un segundo proceso durante una pausa larga.
func OwnsLock(basePath, nodeID string) bool {
	path, err := lockPath(basePath)
	if err != nil {
		return false
	}
	leaderLocksMu.Lock()
	defer leaderLocksMu.Unlock()
	held, ok := leaderLocks[path]
	return ok && held.nodeID == nodeID && held.lock != nil
}

// ReleaseLock libera la elección de liderazgo de forma ordenada. Ante un cierre abrupto,
// el kernel libera el bloqueo al cerrar el proceso.
func ReleaseLock(basePath, nodeID string) error {
	path, err := lockPath(basePath)
	if err != nil {
		return err
	}
	leaderLocksMu.Lock()
	defer leaderLocksMu.Unlock()
	held, ok := leaderLocks[path]
	if !ok || held.nodeID != nodeID {
		return nil
	}
	delete(leaderLocks, path)
	return held.lock.release()
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
	return replaceFile(tmp, path)
}
