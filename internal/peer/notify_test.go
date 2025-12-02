package peer

import (
	"testing"

	"p2p-chat/internal/message"
)

func TestMaybeNotifyDirectMessage(t *testing.T) {
	app, sink, _ := newTestAppContext()
	app.identity.SetDisplay("Alice")
	msg := message.Message{MsgID: "1", Type: msgTypeDM, From: "Bob", To: "Alice"}
	app.maybeNotify(msg)
	notes := sink.Notifications()
	if len(notes) != 1 || notes[0].Level != "dm" {
		t.Fatalf("expected dm notification, got %+v", notes)
	}
}

func TestMaybeNotifyMentions(t *testing.T) {
	app, sink, _ := newTestAppContext()
	app.identity.SetDisplay("Alice")
	msg := message.Message{MsgID: "2", From: "Bob", Content: "hi alice"}
	app.maybeNotify(msg)
	notes := sink.Notifications()
	if len(notes) != 1 || notes[0].Level != "mention" {
		t.Fatalf("expected mention notification, got %+v", notes)
	}
}
