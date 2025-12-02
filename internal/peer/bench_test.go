package peer

import (
	"testing"
	"time"

	"p2p-chat/internal/message"
)

func BenchmarkMsgCacheSeen(b *testing.B) {
	cache := newMsgCache(time.Minute)
	ids := make([]string, 1024)
	for i := range ids {
		ids[i] = newMsgID()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cache.Seen(ids[i%len(ids)]) {
			cache.mu.Lock()
			delete(cache.seen, ids[i%len(ids)])
			cache.mu.Unlock()
		}
	}
}

func BenchmarkHistoryBufferAdd(b *testing.B) {
	buf := newHistory(512)
	msg := message.Message{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.MsgID = newMsgID()
		buf.Add(msg)
	}
}
