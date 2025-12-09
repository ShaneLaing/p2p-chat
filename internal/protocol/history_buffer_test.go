package protocol

import (
	"testing"

	"p2p-chat/internal/message"
)

func TestHistoryBufferTrimsOldEntries(t *testing.T) {
	buf := NewHistoryBuffer(3)
	for i := 0; i < 5; i++ {
		buf.Add(message.Message{MsgID: string(rune('a' + i))})
	}
	all := buf.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(all))
	}
	if all[0].MsgID != "c" || all[2].MsgID != "e" {
		t.Fatalf("unexpected order: %+v", all)
	}
}
