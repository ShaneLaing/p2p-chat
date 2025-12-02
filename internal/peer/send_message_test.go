package peer

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"p2p-chat/internal/message"
)

func TestSendChatMessageUpdatesState(t *testing.T) {
	app, sink, _ := newTestAppContext()
	sendChatMessage(app, "hello world")
	if len(app.history.All()) != 1 {
		t.Fatalf("expected history to contain the chat message")
	}
	msg := sink.LastMessage()
	if msg.Content != "hello world" || msg.Type != msgTypeChat {
		t.Fatalf("unexpected message %+v", msg)
	}
	if len(app.ack.pending) != 1 {
		t.Fatalf("expected ack tracker to track message")
	}
}

func TestSendDirectMessageTargetsRecipient(t *testing.T) {
	app, sink, _ := newTestAppContext()
	app.directory.Record("Bob", "10.0.0.2:9001")
	sendDirectMessage(app, "Bob", "secret")
	msg := sink.LastMessage()
	if msg.Type != msgTypeDM {
		t.Fatalf("expected DM type")
	}
	if msg.To != "Bob" || msg.ToAddr != "10.0.0.2:9001" {
		t.Fatalf("expected directory resolution in dm: %+v", msg)
	}
	if msg.Content != "secret" {
		t.Fatalf("expected content preserved")
	}
}

func TestPersistExternalSendsRequest(t *testing.T) {
	app, _, _ := newTestAppContext()
	app.identity.SetAuth("alice", "token")
	var wg sync.WaitGroup
	wg.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("missing auth header: %s", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	app.authAPI = srv.URL
	msg := message.Message{From: "alice", Content: "hi"}
	app.persistExternal(msg, "")
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("external persistence timed out")
	}
}
