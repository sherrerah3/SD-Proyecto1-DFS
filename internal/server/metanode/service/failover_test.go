package service

import (
	"testing"
	"time"
)

func TestTryAcquireLockLibre(t *testing.T) {
	dir := t.TempDir()

	// Carpeta sin lock previo: el primer nodo debe conseguirlo.
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("node-A should have acquired the free lock")
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock should have pertenecer a nodo-A")
	}
}

func TestTryAcquireLockOcupadoReciente(t *testing.T) {
	dir := t.TempDir()

	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed: node-A did not acquire the lock")
	}

	// nodo-B no puede robar un lock reciente de nodo-A.
	if TryAcquireLock(dir, "nodo-B", 10*time.Second) {
		t.Fatalf("node-B should not have acquired another node's recent lock")
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock should have seguir siendo de nodo-A")
	}
}

func TestTryAcquireLockVencido(t *testing.T) {
	dir := t.TempDir()

	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed: node-A did not acquire the lock")
	}

	// Con un TTL de 0, el lock de A se considera vencido y B puede tomarlo.
	time.Sleep(1 * time.Millisecond)
	if !TryAcquireLock(dir, "nodo-B", 0) {
		t.Fatalf("node-B should have acquired an expired lock")
	}
	if !OwnsLock(dir, "nodo-B") {
		t.Fatalf("el lock should have pasar a nodo-B")
	}
}

func TestRenewLockMismoDueno(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed")
	}

	if err := RenewLock(dir, "nodo-A"); err != nil {
		t.Fatalf("RenewLock failed: %v", err)
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("tras renovar, el lock should have seguir siendo de nodo-A")
	}
}

func TestIsLeaderAlive(t *testing.T) {
	dir := t.TempDir()

	// Sin heartbeat escrito, no hay líder vivo.
	if IsLeaderAlive(dir, 15*time.Second) {
		t.Fatalf("without a heartbeat there should be no live leader")
	}

	// Tras escribir uno, el líder está vivo dentro del umbral.
	if err := writeHeartbeat(dir); err != nil {
		t.Fatalf("writeHeartbeat failed: %v", err)
	}
	if !IsLeaderAlive(dir, 15*time.Second) {
		t.Fatalf("with a recent heartbeat the leader should be alive")
	}

	// Con umbral 0, cualquier heartbeat pasado cuenta como muerto.
	time.Sleep(1 * time.Millisecond)
	if IsLeaderAlive(dir, 0) {
		t.Fatalf("with a threshold of 0, the leader should be considered dead")
	}
}

func TestOwnsLockAjeno(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed")
	}

	if OwnsLock(dir, "nodo-B") {
		t.Fatalf("nodo-B no es dueño del lock")
	}
}
