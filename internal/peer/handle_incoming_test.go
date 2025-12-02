package peer

import (
	"testing"

	"p2p-chat/internal/message"
)

func TestProcessIncomingStoresChat(t *testing.T) {
	app, sink, _ := newTestAppContext()
	msg := message.Message{MsgID: "m1", From: "Bob", Content: "hi"}
	app.processIncoming(msg)
	if len(app.history.All()) != 1 {
		t.Fatalf("expected history to record message")
	}
	if len(sink.messages) != 1 {
		t.Fatalf("expected sink to show message")
	}
	if snapshot := app.metrics.Snapshot(); snapshot.Seen != 1 {
		t.Fatalf("expected metrics to record seen message")
	}
}

func TestProcessIncomingHonorsBlocklist(t *testing.T) {
	app, sink, _ := newTestAppContext()
	app.blocklist.Add("Bob")
	app.processIncoming(message.Message{MsgID: "m2", From: "Bob", Content: "hi"})
	if len(sink.messages) != 0 {
		t.Fatalf("blocked sender should be suppressed")
	}
}

func TestProcessIncomingAckRemovesPending(t *testing.T) {
	app, _, _ := newTestAppContext()
	app.ack.pending = map[string]*pendingAck{"ackme": {msg: message.Message{MsgID: "ackme"}}}
	app.processIncoming(message.Message{Type: msgTypeAck, AckFor: "ackme"})
	if len(app.ack.pending) != 0 {
		t.Fatalf("expected ack to remove pending message")
	}
}
