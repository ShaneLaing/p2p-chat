package peer

import (
	"path/filepath"
	"testing"
	"time"

	"p2p-chat/internal/message"
)

func TestHistoryStoreAppendAndRecent(t *testing.T) {
	tempDir := t.TempDir()
	store, err := openHistoryStore(filepath.Join(tempDir, "history.db"))
	if err != nil {
		t.Fatalf("openHistoryStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	base := time.Now()
	msgs := []message.Message{
		{MsgID: "one", Content: "first", Timestamp: base.Add(-3 * time.Second)},
		{MsgID: "two", Content: "second", Timestamp: base.Add(-2 * time.Second)},
		{MsgID: "three", Content: "third", Timestamp: base.Add(-1 * time.Second)},
	}

	for _, msg := range msgs {
		if err := store.Append(msg); err != nil {
			t.Fatalf("append %s: %v", msg.MsgID, err)
		}
	}

	recent, err := store.Recent(2)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(recent))
	}
	if recent[0].MsgID != "three" || recent[1].MsgID != "two" {
		t.Fatalf("unexpected order: %+v", []string{recent[0].MsgID, recent[1].MsgID})
	}
}

func TestHistoryStoreRecentLimitZero(t *testing.T) {
	store := &historyStore{}
	msgs, err := store.Recent(0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil slice when limit <= 0, got %v", msgs)
	}
}
