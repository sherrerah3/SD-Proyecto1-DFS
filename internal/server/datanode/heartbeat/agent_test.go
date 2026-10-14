package heartbeat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"dfsha/internal/server/datanode/blockstore"
)

// fakeReporter captura los reports recibidos para inspeccionarlos en los tests.
type fakeReporter struct {
	mu         sync.Mutex
	registered []Report
	beats      []Report
}

func (f *fakeReporter) Register(ctx context.Context, r Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, r)
	return nil
}

func (f *fakeReporter) Beat(ctx context.Context, r Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beats = append(f.beats, r)
	return nil
}

func (f *fakeReporter) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registered), len(f.beats)
}

func newStoreWithBlocks(t *testing.T, n int) blockstore.BlockStore {
	t.Helper()

	store, err := blockstore.NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("preparación falló: %v", err)
	}

	for i := 0; i < n; i++ {
		id := "blk-" + string(rune('a'+i))
		if _, err := store.Write(id, strings.NewReader(id)); err != nil {
			t.Fatalf("preparación falló: %v", err)
		}
	}

	return store
}

func TestAgentRegistersAndBeats(t *testing.T) {
	store := newStoreWithBlocks(t, 3)
	reporter := &fakeReporter{}

	agent := NewAgent("dn-1", "127.0.0.1:9000", store, reporter, func() int64 { return 1000 })
	agent.SetInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = agent.Run(ctx)
		close(done)
	}()

	// Se deja correr lo suficiente para registrar y emitir varios latidos.
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	registered, beats := reporter.counts()
	if registered != 1 {
		t.Fatalf("se esperaba 1 registro, se obtuvieron %d", registered)
	}

	if beats == 0 {
		t.Fatalf("se esperaba al menos un latido")
	}
}

func TestAgentReportsStoreState(t *testing.T) {
	store := newStoreWithBlocks(t, 2)
	reporter := &fakeReporter{}

	agent := NewAgent("dn-1", "127.0.0.1:9000", store, reporter, func() int64 { return 4096 })

	report := agent.snapshot()

	if report.DataNodeID != "dn-1" {
		t.Fatalf("DataNodeID = %q, se esperaba dn-1", report.DataNodeID)
	}

	if report.BlockCount != 2 {
		t.Fatalf("BlockCount = %d, se esperaba 2", report.BlockCount)
	}

	if report.FreeBytes != 4096 {
		t.Fatalf("FreeBytes = %d, se esperaba 4096", report.FreeBytes)
	}
}
