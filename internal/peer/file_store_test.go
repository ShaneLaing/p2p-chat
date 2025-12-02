package peer

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreSaveAndRetrieve(t *testing.T) {
	base := t.TempDir()
	store, err := openFileStore(filepath.Join(base, "files.db"), filepath.Join(base, "files"))
	if err != nil {
		t.Fatalf("openFileStore error: %v", err)
	}
	defer store.Close()

	rec, err := store.Save("../sample.txt", "alice", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("save error: %v", err)
	}
	if rec.Name != "sample.txt" {
		t.Fatalf("expected sanitized name, got %s", rec.Name)
	}
	if rec.Size != int64(len("hello world")) {
		t.Fatalf("unexpected size: %d", rec.Size)
	}
	if rec.ShareKey == "" {
		t.Fatalf("expected share key to be set")
	}

	list, err := store.List(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list error: %v len=%d", err, len(list))
	}
	if list[0].ID != rec.ID {
		t.Fatalf("list returned different record")
	}

	entry, file, err := store.Open(rec.ID)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected file contents: %s", data)
	}
	if entry.Path == "" {
		t.Fatalf("expected entry path recorded")
	}
}

func TestSanitizeFileName(t *testing.T) {
	if got := sanitizeFileName("../../etc/passwd"); got != "passwd" {
		t.Fatalf("expected base filename, got %s", got)
	}
	if got := sanitizeFileName("\\"); got != "" {
		t.Fatalf("expected empty for root tokens, got %s", got)
	}
}
