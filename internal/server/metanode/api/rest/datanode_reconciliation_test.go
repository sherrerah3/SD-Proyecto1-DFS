package rest

import (
	"testing"

	"dfsha/internal/server/datanode/blockstore"
	"dfsha/internal/server/metanode/store"
)

func TestReconcileDataNodeInventoryQueuesStaleBlocksAndAcknowledgesDeletion(t *testing.T) {
	s := store.NewMemoryStore()
	if err := s.RegisterDataNode(store.DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("register DataNode: %v", err)
	}
	if err := s.CreateFile(store.FileMetadata{
		Owner: "alice", Path: "/current.txt", BlockIDs: []string{"current-block"},
	}); err != nil {
		t.Fatalf("create file metadata: %v", err)
	}
	if err := s.SaveBlockLocation(store.BlockMetadata{
		BlockID: "current-block", DataNodes: []string{"dn-1"},
		Confirmations: map[string]string{"dn-1": "abc"},
	}); err != nil {
		t.Fatalf("save current block location: %v", err)
	}
	if err := s.SaveBlockLocation(store.BlockMetadata{
		BlockID: "obsolete-block", DataNodes: []string{"dn-1"},
	}); err != nil {
		t.Fatalf("save obsolete block location: %v", err)
	}

	inventory := []blockstore.BlockReport{
		{BlockID: "current-block", SHA256: "abc"},
		{BlockID: "obsolete-block", SHA256: "old"},
	}
	if err := reconcileDataNodeInventory(s, "dn-1", inventory); err != nil {
		t.Fatalf("reconcile inventory: %v", err)
	}

	toDelete, err := pendingDeletionsForReport(s, "dn-1", inventory)
	if err != nil {
		t.Fatalf("collect deletion commands: %v", err)
	}
	if len(toDelete) != 1 || toDelete[0] != "obsolete-block" {
		t.Fatalf("expected only obsolete-block to be deleted, got %v", toDelete)
	}

	// After the next report confirms that the block is gone, the queue is acked.
	if err := reconcileDataNodeInventory(s, "dn-1", []blockstore.BlockReport{{BlockID: "current-block", SHA256: "abc"}}); err != nil {
		t.Fatalf("reconcile updated inventory: %v", err)
	}
	toDelete, err = pendingDeletionsForReport(s, "dn-1", []blockstore.BlockReport{{BlockID: "current-block", SHA256: "abc"}})
	if err != nil {
		t.Fatalf("ack deletion: %v", err)
	}
	if len(toDelete) != 0 {
		t.Fatalf("expected no repeated deletions after acknowledgement, got %v", toDelete)
	}
	pending, err := s.ListBlockDeletions("dn-1")
	if err != nil {
		t.Fatalf("list pending deletions: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected deletion queue to be empty, got %v", pending)
	}
}

func TestEmptyMetaNodeCatalogQueuesAllReportedBlocksAsFreeable(t *testing.T) {
	// A freshly reset MetaNode has no file or block catalog. Every on-disk
	// block reported by a DataNode is therefore unreferenced and can be freed.
	s := store.NewMemoryStore()
	if err := s.RegisterDataNode(store.DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("register DataNode: %v", err)
	}

	inventory := []blockstore.BlockReport{
		{BlockID: "old-block-a", SHA256: "hash-a"},
		{BlockID: "old-block-b", SHA256: "hash-b"},
	}
	if err := reconcileDataNodeInventory(s, "dn-1", inventory); err != nil {
		t.Fatalf("reconcile inventory against empty catalog: %v", err)
	}

	toDelete, err := pendingDeletionsForReport(s, "dn-1", inventory)
	if err != nil {
		t.Fatalf("list blocks to free: %v", err)
	}
	if len(toDelete) != 2 {
		t.Fatalf("expected both unreferenced blocks to be freed, got %v", toDelete)
	}

	// After DataNode deletion, its next inventory report confirms the blocks
	// are absent and the MetaNode may acknowledge the durable deletion queue.
	if err := reconcileDataNodeInventory(s, "dn-1", []blockstore.BlockReport{}); err != nil {
		t.Fatalf("reconcile post-deletion inventory: %v", err)
	}
	toDelete, err = pendingDeletionsForReport(s, "dn-1", []blockstore.BlockReport{})
	if err != nil {
		t.Fatalf("acknowledge freed blocks: %v", err)
	}
	if len(toDelete) != 0 {
		t.Fatalf("expected no further deletions after confirmation, got %v", toDelete)
	}
	pending, err := s.ListBlockDeletions("dn-1")
	if err != nil {
		t.Fatalf("list pending deletions: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected the deletion queue to be empty, got %v", pending)
	}
}

func TestReconcileDataNodeInventoryRemovesMissingConfirmedReplica(t *testing.T) {
	s := store.NewMemoryStore()
	if err := s.RegisterDataNode(store.DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("register DataNode: %v", err)
	}
	if err := s.CreateFile(store.FileMetadata{Owner: "alice", Path: "/current.txt", BlockIDs: []string{"current-block"}}); err != nil {
		t.Fatalf("create file metadata: %v", err)
	}
	if err := s.SaveBlockLocation(store.BlockMetadata{
		BlockID: "current-block", DataNodes: []string{"dn-1", "dn-2"},
		Confirmations: map[string]string{"dn-1": "abc", "dn-2": "abc"},
	}); err != nil {
		t.Fatalf("save current block location: %v", err)
	}

	if err := reconcileDataNodeInventory(s, "dn-1", []blockstore.BlockReport{}); err != nil {
		t.Fatalf("reconcile empty inventory: %v", err)
	}
	loc, err := s.GetBlockLocation("current-block")
	if err != nil {
		t.Fatalf("get current block location: %v", err)
	}
	if containsDataNode(loc.DataNodes, "dn-1") {
		t.Fatalf("missing DataNode replica should no longer be advertised: %v", loc.DataNodes)
	}
	if !containsDataNode(loc.DataNodes, "dn-2") {
		t.Fatalf("healthy replica should be retained: %v", loc.DataNodes)
	}
}

func TestPendingDeletionDoesNotDeleteCurrentlyReferencedReplica(t *testing.T) {
	s := store.NewMemoryStore()
	if err := s.RegisterDataNode(store.DataNodeInfo{ID: "dn-1", Address: "127.0.0.1:9000"}); err != nil {
		t.Fatalf("register DataNode: %v", err)
	}
	if err := s.CreateFile(store.FileMetadata{
		Owner: "alice", Path: "/current.txt", BlockIDs: []string{"live-block"},
	}); err != nil {
		t.Fatalf("create file metadata: %v", err)
	}
	if err := s.SaveBlockLocation(store.BlockMetadata{
		BlockID: "live-block", DataNodes: []string{"dn-1"},
	}); err != nil {
		t.Fatalf("save current block location: %v", err)
	}
	if err := s.EnqueueBlockDeletion("dn-1", "live-block"); err != nil {
		t.Fatalf("queue stale deletion: %v", err)
	}

	toDelete, err := pendingDeletionsForReport(s, "dn-1", []blockstore.BlockReport{{BlockID: "live-block"}})
	if err != nil {
		t.Fatalf("collect deletion commands: %v", err)
	}
	if len(toDelete) != 0 {
		t.Fatalf("a currently referenced replica must not be deleted, got %v", toDelete)
	}
	pending, err := s.ListBlockDeletions("dn-1")
	if err != nil {
		t.Fatalf("list pending deletions: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("obsolete deletion request should be acknowledged, got %v", pending)
	}
}
