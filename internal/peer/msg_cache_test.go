package peer

import (
	"testing"
	"time"
)

func TestMsgCacheSeenRespectsTTL(t *testing.T) {
	cache := newMsgCache(25 * time.Millisecond)
	if cache.Seen("msg") {
		t.Fatalf("first Seen call should miss cache")
	}
	if !cache.Seen("msg") {
		t.Fatalf("second Seen call within ttl should hit cache")
	}
	time.Sleep(30 * time.Millisecond)
	if cache.Seen("msg") {
		t.Fatalf("entry should expire after ttl")
	}
}

func TestMsgCacheIgnoresEmptyIDs(t *testing.T) {
	cache := newMsgCache(time.Minute)
	if cache.Seen("") {
		t.Fatalf("empty id should never be tracked")
	}
}
