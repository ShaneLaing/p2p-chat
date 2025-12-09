package protocol

import (
	"testing"

	"p2p-chat/internal/message"
)

func TestProcessIncomingStoresChat(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	msg := message.Message{MsgID: "m1", From: "Bob", Content: "hi"}
	rt.processIncoming(msg)
	if len(rt.history.All()) != 1 {
		t.Fatalf("expected history to record message")
	}
	if len(sink.messages) != 1 {
		t.Fatalf("expected sink to show message")
	}
	if snapshot := rt.metrics.Snapshot(); snapshot.Seen != 1 {
		t.Fatalf("expected metrics to record seen message")
	}
}

func TestProcessIncomingHonorsBlocklist(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	rt.blocklist.Add("Bob")
	rt.processIncoming(message.Message{MsgID: "m2", From: "Bob", Content: "hi"})
	if len(sink.messages) != 0 {
		t.Fatalf("blocked sender should be suppressed")
	}
}

func TestProcessIncomingAckRemovesPending(t *testing.T) {
	rt, _, _ := newTestRuntime(t)
	rt.ack.pending = map[string]*pendingAck{"ackme": {msg: message.Message{MsgID: "ackme"}}}
	rt.processIncoming(message.Message{Type: MsgTypeAck, AckFor: "ackme"})
	if len(rt.ack.pending) != 0 {
		t.Fatalf("expected ack to remove pending message")
	}
}
