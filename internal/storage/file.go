package storage

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

const filesBucket = "files"

// FileRecord is exported to UIs so downloads can be surfaced in chat history.
type FileRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Uploader  string    `json:"uploader"`
	Mime      string    `json:"mime,omitempty"`
	ShareKey  string    `json:"share_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// fileEntry keeps the on-disk path private to the store.
type fileEntry struct {
	FileRecord
	Path string `json:"path"`
}

// FileStore persists uploads on disk and records their metadata in BoltDB.
type FileStore struct {
	db  *bbolt.DB
	dir string
}

func OpenFileStore(dbPath, dir string) (*FileStore, error) {
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
	return &FileStore{db: db, dir: dir}, nil
}

func (s *FileStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *FileStore) Save(originalName, uploader string, src io.Reader) (FileRecord, error) {
	if s == nil || s.db == nil {
		return FileRecord{}, fmt.Errorf("file store not initialized")
	}
	cleaned := sanitizeFileName(originalName)
	if cleaned == "" {
		cleaned = "upload.bin"
	}
	id := newFileID()
	path := filepath.Join(s.dir, id)
	dst, err := os.Create(path)
	if err != nil {
		return FileRecord{}, err
	}
	defer dst.Close()
	size, err := io.Copy(dst, src)
	if err != nil {
		return FileRecord{}, err
	}
	mime := detectMime(path)
	entry := fileEntry{
		FileRecord: FileRecord{
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
		return FileRecord{}, err
	}
	err = s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(filesBucket))
		return bucket.Put([]byte(entry.ID), data)
	})
	if err != nil {
		return FileRecord{}, err
	}
	return entry.FileRecord, nil
}

func (s *FileStore) List(limit int) ([]FileRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	records := make([]FileRecord, 0, limit)
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
			records = append(records, entry.FileRecord)
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

func (s *FileStore) Get(id string) (*fileEntry, error) {
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

func (s *FileStore) Open(id string) (*fileEntry, *os.File, error) {
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

func newFileID() string {
	b := make([]byte, 16)
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
