package peer

import "testing"

func TestBuildDownloadURLIncludesShareKey(t *testing.T) {
	wb := &webBridge{addr: "127.0.0.1:8081"}
	url := buildDownloadURL(wb, fileRecord{ID: "abc", ShareKey: "secret"})
	if url != "http://127.0.0.1:8081/api/files/abc?key=secret" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestShareFileRequiresWeb(t *testing.T) {
	app, _, _ := newTestAppContext()
	err := shareFile(app, fileRecord{}, "")
	if err == nil {
		t.Fatalf("expected error when web ui disabled")
	}
}

func TestShareFilePersistsMessage(t *testing.T) {
	app, sink, _ := newTestAppContext()
	app.web = &webBridge{addr: "127.0.0.1:8081"}
	app.directory.Record("Bob", "10.0.0.2:9001")

	record := fileRecord{ID: "file1", Name: "report.pdf", Size: 42, Mime: "application/pdf", ShareKey: "k"}
	if err := shareFile(app, record, "Bob"); err != nil {
		t.Fatalf("shareFile returned error: %v", err)
	}

	msg := sink.LastMessage()
	if msg.Type != msgTypeFile {
		t.Fatalf("expected file message, got %s", msg.Type)
	}
	if msg.To != "Bob" || msg.ToAddr != "10.0.0.2:9001" {
		t.Fatalf("expected dm targeting Bob, got %+v", msg)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].URL == "" {
		t.Fatalf("attachment url missing")
	}
	if snapshot := app.metrics.Snapshot(); snapshot.Sent != 1 {
		t.Fatalf("expected metrics to record sent message: %+v", snapshot)
	}
	if len(app.history.All()) != 1 {
		t.Fatalf("expected history to contain file message")
	}
}
