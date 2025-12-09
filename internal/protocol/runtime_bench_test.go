package protocol

import (
	"testing"
	"time"

	"p2p-chat/internal/message"
)

func BenchmarkMsgCacheSeen(b *testing.B) {
	cache := NewMsgCache(time.Minute)
	ids := make([]string, 1024)
	for i := range ids {
		ids[i] = NewMsgID()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % len(ids)
		if cache.Seen(ids[idx]) {
			ids[idx] = NewMsgID()
		}
	}
}

func BenchmarkHistoryBufferAdd(b *testing.B) {
	buf := NewHistoryBuffer(512)
	msg := message.Message{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.MsgID = NewMsgID()
		buf.Add(msg)
	}
}
