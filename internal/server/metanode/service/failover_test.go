package service

import (
	"testing"
	"time"
)

func TestTryAcquireLockLibre(t *testing.T) {
	dir := t.TempDir()

	// Carpeta sin lock previo: el primer nodo debe conseguirlo.
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("nodo-A debió adquirir el lock libre")
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock debía pertenecer a nodo-A")
	}
}

func TestTryAcquireLockOcupadoReciente(t *testing.T) {
	dir := t.TempDir()

	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("preparación falló: nodo-A no adquirió el lock")
	}

	// nodo-B no puede robar un lock reciente de nodo-A.
	if TryAcquireLock(dir, "nodo-B", 10*time.Second) {
		t.Fatalf("nodo-B no debió adquirir un lock reciente de otro")
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("el lock debía seguir siendo de nodo-A")
	}
}

func TestTryAcquireLockVencido(t *testing.T) {
	dir := t.TempDir()

	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("preparación falló: nodo-A no adquirió el lock")
	}

	// Con un TTL de 0, el lock de A se considera vencido y B puede tomarlo.
	time.Sleep(1 * time.Millisecond)
	if !TryAcquireLock(dir, "nodo-B", 0) {
		t.Fatalf("nodo-B debió tomar un lock vencido")
	}
	if !OwnsLock(dir, "nodo-B") {
		t.Fatalf("el lock debía pasar a nodo-B")
	}
}

func TestRenewLockMismoDueno(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("preparación falló")
	}

	if err := RenewLock(dir, "nodo-A"); err != nil {
		t.Fatalf("RenewLock falló: %v", err)
	}
	if !OwnsLock(dir, "nodo-A") {
		t.Fatalf("tras renovar, el lock debía seguir siendo de nodo-A")
	}
}

func TestIsLeaderAlive(t *testing.T) {
	dir := t.TempDir()

	// Sin heartbeat escrito, no hay líder vivo.
	if IsLeaderAlive(dir, 15*time.Second) {
		t.Fatalf("sin heartbeat no debía haber líder vivo")
	}

	// Tras escribir uno, el líder está vivo dentro del umbral.
	if err := writeHeartbeat(dir); err != nil {
		t.Fatalf("writeHeartbeat falló: %v", err)
	}
	if !IsLeaderAlive(dir, 15*time.Second) {
		t.Fatalf("con heartbeat reciente el líder debía estar vivo")
	}

	// Con umbral 0, cualquier heartbeat pasado cuenta como muerto.
	time.Sleep(1 * time.Millisecond)
	if IsLeaderAlive(dir, 0) {
		t.Fatalf("con umbral 0 el líder debía contarse como muerto")
	}
}

func TestOwnsLockAjeno(t *testing.T) {
	dir := t.TempDir()
	if !TryAcquireLock(dir, "nodo-A", 10*time.Second) {
		t.Fatalf("preparación falló")
	}

	if OwnsLock(dir, "nodo-B") {
		t.Fatalf("nodo-B no es dueño del lock")
	}
}
