package protocol

import (
	"testing"
	"time"
)

func TestBlockListAddRemove(t *testing.T) {
	bl := NewBlockList()
	bl.Add("alice")
	if !bl.Blocks("alice", "") {
		t.Fatalf("expected alice to be blocked by name")
	}
	bl.Add("10.0.0.5")
	if !bl.Blocks("", "10.0.0.5") {
		t.Fatalf("expected addr to be blocked")
	}
	bl.Remove("alice")
	if bl.Blocks("alice", "") {
		t.Fatalf("expected alice to be removed")
	}
	got := bl.List()
	if len(got) != 1 || got[0] != "10.0.0.5" {
		t.Fatalf("unexpected list contents: %+v", got)
	}
}

func TestPeerDirectoryRecordAndResolve(t *testing.T) {
	dir := NewPeerDirectory()
	dir.Record("Alice", "10.0.0.2:9001")
	addr, name, ok := dir.Resolve("alice")
	if !ok || addr != "10.0.0.2:9001" || name != "Alice" {
		t.Fatalf("resolve by name failed: %v %s %s", ok, addr, name)
	}
	_, name, ok = dir.Resolve("10.0.0.2:9001")
	if !ok || name != "Alice" {
		t.Fatalf("resolve by addr failed")
	}
}

func TestPeerDirectoryMarkActiveAndSnapshot(t *testing.T) {
	dir := NewPeerDirectory()
	dir.Record("Alice", "10.0.0.2:9001")
	dir.Record("Bob", "10.0.0.3:9001")
	dir.MarkActive([]string{"10.0.0.2:9001"})
	snapshot := dir.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("expected two peers in snapshot")
	}
	dir.mu.Lock()
	if entry, ok := dir.byAddr["10.0.0.3:9001"]; ok {
		entry.LastSeen = time.Now().Add(-(presenceGrace + time.Second))
	}
	dir.mu.Unlock()
	dir.MarkActive(nil)
	_, _, ok := dir.Resolve("10.0.0.3:9001")
	if !ok {
		t.Fatalf("bob should still resolve")
	}
	snapshot = dir.Snapshot()
	for _, peer := range snapshot {
		if peer.Name == "Bob" && peer.Online {
			t.Fatalf("bob should be offline after grace period")
		}
	}
}
