package peer

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// filesBucket stores `fileEntry` blobs keyed by the stable file ID so lookups
// for downloads do not require scanning the bucket.
const filesBucket = "files"

// fileRecord is the public metadata we share with the web UI / API callers.
type fileRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Uploader  string    `json:"uploader"`
	Mime      string    `json:"mime,omitempty"`
	ShareKey  string    `json:"share_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// fileEntry persists the on-disk path in Bolt for internal lookups.
type fileEntry struct {
	fileRecord
	Path string `json:"path"`
}

type fileStore struct {
	db  *bbolt.DB
	dir string
}

func openFileStore(dbPath, dir string) (*fileStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := bbolt.Open(dbPath, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(filesBucket))
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &fileStore{db: db, dir: dir}, nil
}

func (s *fileStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Save writes the uploaded content to disk and stores the metadata.
func (s *fileStore) Save(originalName, uploader string, src io.Reader) (fileRecord, error) {
	if s == nil || s.db == nil {
		return fileRecord{}, fmt.Errorf("file store not initialized")
	}
	cleaned := sanitizeFileName(originalName)
	if cleaned == "" {
		cleaned = "upload.bin"
	}
	id := newMsgID()
	path := filepath.Join(s.dir, id)
	dst, err := os.Create(path)
	if err != nil {
		return fileRecord{}, err
	}
	defer dst.Close()
	size, err := io.Copy(dst, src)
	if err != nil {
		return fileRecord{}, err
	}
	mime := detectMime(path)
	entry := fileEntry{
		fileRecord: fileRecord{
			ID:        id,
			Name:      cleaned,
			Size:      size,
			Uploader:  uploader,
			Mime:      mime,
			ShareKey:  newShareKey(),
			CreatedAt: time.Now().UTC(),
		},
		Path: path,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fileRecord{}, err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(filesBucket))
		return bucket.Put([]byte(entry.ID), data)
	})
	if err != nil {
		return fileRecord{}, err
	}
	return entry.fileRecord, nil
}

func (s *fileStore) List(limit int) ([]fileRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	records := make([]fileRecord, 0, limit)
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(filesBucket))
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var entry fileEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}
			records = append(records, entry.fileRecord)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *fileStore) Get(id string) (*fileEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("file store not initialized")
	}
	var result *fileEntry
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(filesBucket))
		if bucket == nil {
			return fmt.Errorf("missing bucket")
		}
		data := bucket.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("file not found")
		}
		var entry fileEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return err
		}
		result = &entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *fileStore) Open(id string) (*fileEntry, *os.File, error) {
	entry, err := s.Get(id)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(entry.Path)
	if err != nil {
		return nil, nil, err
	}
	return entry, f, nil
}

func sanitizeFileName(name string) string {
	cleaned := filepath.Base(name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == "/" || cleaned == "\\" || cleaned == string(filepath.Separator) {
		return ""
	}
	return cleaned
}

func newShareKey() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

func detectMime(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}
