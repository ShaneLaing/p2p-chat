package protocol

import (
	"testing"

	"p2p-chat/internal/message"
)

func TestMaybeNotifyDirectMessage(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	rt.identity.SetDisplay("Alice")
	msg := message.Message{MsgID: "1", Type: MsgTypeDM, From: "Bob", To: "Alice"}
	rt.maybeNotify(msg)
	notes := sink.notificationCopy()
	if len(notes) != 1 || notes[0].Level != "dm" {
		t.Fatalf("expected dm notification, got %+v", notes)
	}
}

func TestMaybeNotifyMentions(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	rt.identity.SetDisplay("Alice")
	msg := message.Message{MsgID: "2", From: "Bob", Content: "hi alice"}
	rt.maybeNotify(msg)
	notes := sink.notificationCopy()
	if len(notes) != 1 || notes[0].Level != "mention" {
		t.Fatalf("expected mention notification, got %+v", notes)
	}
}
