package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"p2p-chat/internal/message"
)

// historyBucket keeps JSON-encoded message blobs keyed by
// `<timestamp>-<msgID>` so iterating with a reverse cursor returns newest-first.
const historyBucket = "messages"

type historyStore struct {
	db *bbolt.DB
}

func openHistoryStore(path string) (*historyStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(historyBucket))
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &historyStore{db: db}, nil
}

func (s *historyStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *historyStore) Append(msg message.Message) error {
	if s == nil || s.db == nil {
		return nil
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(historyBucket))
		key := []byte(fmt.Sprintf("%020d-%s", msg.Timestamp.UnixNano(), msg.MsgID))
		return bucket.Put(key, data)
	})
}

func (s *historyStore) Recent(limit int) ([]message.Message, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}
	var out []message.Message
	err := s.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(historyBucket))
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for k, v := cursor.Last(); k != nil && limit > 0; k, v = cursor.Prev() {
			var msg message.Message
			if err := json.Unmarshal(v, &msg); err == nil {
				out = append(out, msg)
			}
			limit--
		}
		return nil
	})
	return out, err
}
