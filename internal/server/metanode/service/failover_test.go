package service

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestTryAcquireLockLibre(t *testing.T) {
	dir := t.TempDir()

	// Carpeta sin lock previo: el primer nodo debe conseguirlo.
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("node-A should have acquired the free lock")
	}
	defer ReleaseLock(dir, "nodo-A")
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock should have pertenecer a nodo-A")
	}
}

func TestTryAcquireLockOcupadoReciente(t *testing.T) {
	dir := t.TempDir()

	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed: node-A did not acquire the lock")
	}
	defer ReleaseLock(dir, "nodo-A")

	// nodo-B no puede robar un lock reciente de nodo-A.
	if TryAcquireLock(dir, "nodo-B", 10*time.Second) {
		t.Fatalf("node-B should not have acquired another node's recent lock")
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock should have seguir siendo de nodo-A")
	}
}

func TestLockNoPuedeRobarseMientrasElLiderVive(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed: node-A did not acquire the lock")
	}
	if TryAcquireLock(dir, "nodo-B", 0) {
		t.Fatalf("node-B must not steal the lock just because a TTL expired")
	}
	if err := ReleaseLock(dir, "nodo-A"); err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}
	if !TryAcquireLock(dir, "nodo-B", 10*time.Second) {
		t.Fatalf("node-B should acquire the lock after node-A releases it")
	}
	defer ReleaseLock(dir, "nodo-B")
	if !OwnsLock(dir, "nodo-B") {
		t.Fatalf("lock should now belong to node-B")
	}
}

func TestRenewLockMismoDueno(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("setup failed")
	}
	defer ReleaseLock(dir, "nodo-A")

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
	defer ReleaseLock(dir, "nodo-A")

	if OwnsLock(dir, "nodo-B") {
		t.Fatalf("nodo-B no es dueño del lock")
	}
}

// TestLockHelperProcess is invoked by TestLeaderLockIsExclusiveAcrossProcesses.
// It runs in a separate process so the test covers the OS lock, not just the local map.
func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("DFSHA_LOCK_HELPER") != "1" {
		return
	}
	dir := os.Getenv("DFSHA_LOCK_DIR")
	if TryAcquireLock(dir, "child-process", 0) {
		_ = ReleaseLock(dir, "child-process")
		t.Fatal("a second process acquired a lock that is still held by the parent")
	}
}

func TestLeaderLockIsExclusiveAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "parent-process", time.Second) {
		t.Fatal("parent process could not acquire lock")
	}
	defer ReleaseLock(dir, "parent-process")

	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "DFSHA_LOCK_HELPER=1", "DFSHA_LOCK_DIR="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child-process lock check failed: %v\n%s", err, output)
	}
}
