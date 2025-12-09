package protocol

import (
	"testing"

	"p2p-chat/internal/storage"
	"p2p-chat/internal/ui"
)

func TestBuildDownloadURLIncludesShareKey(t *testing.T) {
	rt, _, _ := newTestRuntime(t)
	web, err := ui.NewWebBridge("127.0.0.1:8081", rt.History(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("web bridge init: %v", err)
	}
	t.Cleanup(web.Close)
	rt.SetWeb(web)
	url := rt.buildDownloadURL(storage.FileRecord{ID: "abc", ShareKey: "secret"})
	expected := "http://127.0.0.1:8081/api/files/abc?key=secret"
	if url != expected {
		t.Fatalf("unexpected url: %s (want %s)", url, expected)
	}
}

func TestShareFileRequiresWeb(t *testing.T) {
	rt, _, _ := newTestRuntime(t)
	err := rt.ShareFile(storage.FileRecord{}, "")
	if err == nil {
		t.Fatalf("expected error when web ui disabled")
	}
}

func TestShareFilePersistsMessage(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	web, err := ui.NewWebBridge("127.0.0.1:8081", rt.History(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("web bridge init: %v", err)
	}
	t.Cleanup(web.Close)
	rt.SetWeb(web)
	rt.directory.Record("Bob", "10.0.0.2:9001")

	record := storage.FileRecord{ID: "file1", Name: "report.pdf", Size: 42, Mime: "application/pdf", ShareKey: "k"}
	if err := rt.ShareFile(record, "Bob"); err != nil {
		t.Fatalf("ShareFile returned error: %v", err)
	}

	msg := sink.lastMessage()
	if msg.Type != MsgTypeFile {
		t.Fatalf("expected file message, got %s", msg.Type)
	}
	if msg.To != "Bob" || msg.ToAddr != "10.0.0.2:9001" {
		t.Fatalf("expected dm targeting Bob, got %+v", msg)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].URL == "" {
		t.Fatalf("attachment url missing")
	}
	if snapshot := rt.metrics.Snapshot(); snapshot.Sent != 1 {
		t.Fatalf("expected metrics to record sent message: %+v", snapshot)
	}
	if len(rt.history.All()) != 1 {
		t.Fatalf("expected history to contain file message")
	}
}

func TestShareFileBroadcastsAttachment(t *testing.T) {
	rt, sink, _ := newTestRuntime(t)
	web, err := ui.NewWebBridge("127.0.0.1:8081", rt.History(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("web bridge init: %v", err)
	}
	t.Cleanup(web.Close)
	rt.SetWeb(web)
	record := storage.FileRecord{ID: "file2", Name: "draft.txt", ShareKey: "k"}
	if err := rt.ShareFile(record, ""); err != nil {
		t.Fatalf("ShareFile returned error: %v", err)
	}
	msg := sink.lastMessage()
	if msg.Type != MsgTypeFile {
		t.Fatalf("expected broadcast file message, got %s", msg.Type)
	}
	if msg.To != "" || msg.ToAddr != "" {
		t.Fatalf("expected broadcast with no direct recipient, got %+v", msg)
	}
	if msg.Content != "shared a file: draft.txt" {
		t.Fatalf("unexpected broadcast content: %s", msg.Content)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].URL == "" {
		t.Fatalf("attachment url missing for broadcast")
	}
}
