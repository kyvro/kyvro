package core

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bucketUsage stores one record per result ID:
//
//	key   = result ID (string)
//	value = 16 bytes: int64 count + int64 lastUsed unix nano
var bucketUsage = []byte("usage")

// Usage is the persisted launch history of a single result ID.
type Usage struct {
	Count    int64
	LastUsed time.Time
}

// Store persists usage history in a bbolt database.
type Store struct {
	db *bolt.DB
}

// OpenStore opens (creating if necessary) the bbolt database at path.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketUsage)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// DefaultDataPath returns os.UserConfigDir()/Kyvro/data.db. A pre-rename
// "Lumo" directory (usage history, plugins, settings) is migrated once via
// rename, preserving all data.
func DefaultDataPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	oldDir := filepath.Join(dir, "Lumo")
	newDir := filepath.Join(dir, "Kyvro")
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		if _, err := os.Stat(oldDir); err == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				return "", fmt.Errorf("migrate config dir Lumo→Kyvro: %w", err)
			}
		}
	}
	return filepath.Join(newDir, "data.db"), nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Record increments the launch count for id and stamps lastUsed=now.
func (s *Store) Record(id string, now time.Time) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketUsage)
		u, err := readUsage(b, []byte(id))
		if err != nil {
			return err
		}
		u.Count++
		u.LastUsed = now
		return b.Put([]byte(id), encodeUsage(u))
	})
}

// Get returns the usage record for id (zero value when absent).
func (s *Store) Get(id string) (Usage, error) {
	var u Usage
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		u, err = readUsage(tx.Bucket(bucketUsage), []byte(id))
		return err
	})
	return u, err
}

// All returns every usage record, keyed by result ID.
func (s *Store) All() (map[string]Usage, error) {
	out := make(map[string]Usage)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketUsage).ForEach(func(k, v []byte) error {
			u, err := decodeUsage(v)
			if err != nil {
				return fmt.Errorf("decode %q: %w", k, err)
			}
			out[string(k)] = u
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func encodeUsage(u Usage) []byte {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], uint64(u.Count))
	binary.BigEndian.PutUint64(buf[8:16], uint64(u.LastUsed.UnixNano()))
	return buf[:]
}

func decodeUsage(v []byte) (Usage, error) {
	if len(v) != 16 {
		return Usage{}, fmt.Errorf("usage record has %d bytes, want 16", len(v))
	}
	return Usage{
		Count:    int64(binary.BigEndian.Uint64(v[0:8])),
		LastUsed: time.Unix(0, int64(binary.BigEndian.Uint64(v[8:16]))),
	}, nil
}

func readUsage(b *bolt.Bucket, key []byte) (Usage, error) {
	v := b.Get(key)
	if v == nil {
		return Usage{}, nil
	}
	return decodeUsage(v)
}

// PutNS stores a string value under key inside the namespace bucket ns
// (created on demand). Namespaces are generic string→string buckets that
// subsystems (e.g. plugin storage) allocate for themselves.
func (s *Store) PutNS(ns, key, value string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(ns))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), []byte(value))
	})
}

// GetNS returns the value stored under key in namespace ns.
func (s *Store) GetNS(ns, key string) (string, bool, error) {
	var (
		val string
		ok  bool
	)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ns))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			val, ok = string(v), true
		}
		return nil
	})
	return val, ok, err
}

// DeleteNS removes key from namespace ns (no-op when absent).
func (s *Store) DeleteNS(ns, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ns))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}
