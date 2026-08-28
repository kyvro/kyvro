// Package indexcache manages the rebuildable JSON index caches
// (cache/app-index.json, cache/folder-index.json). Cache data is never user
// state: corrupt or future-version files degrade to an empty index, and
// every write replaces the file atomically (temp file + rename).
package indexcache

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kyvro/internal/core"
)

const (
	appFileName    = "app-index.json"
	folderFileName = "folder-index.json"
	tmpSuffix      = ".tmp"
)

// Cache reads and writes the index cache files under dir.
type Cache struct {
	mu sync.Mutex
	// dir is the cache directory (created by Open).
	dir string
	// folderEntries is the in-memory mirror of folder-index.json so
	// per-source queries (FolderEntriesForSource) avoid file I/O.
	folderEntries []core.FolderIndexEntry
}

// Open creates dir if needed and preloads folder-index.json. A missing or
// corrupt folder index degrades to empty; the error is only reported when
// the directory itself cannot be created.
func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	c := &Cache{dir: dir}
	if f, err := c.LoadFolderIndex(); err == nil {
		c.folderEntries = f.Entries
	}
	return c, nil
}

// LoadAppIndex returns the cached app index. Missing, corrupt or
// future-version files yield an empty file with a nil error — the caller
// rebuilds via a background rescan.
func (c *Cache) LoadAppIndex() (core.AppIndexFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return readJSON[core.AppIndexFile](filepath.Join(c.dir, appFileName))
}

// SaveAppIndex atomically replaces app-index.json.
func (c *Cache) SaveAppIndex(entries []core.AppIndexEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeJSON(filepath.Join(c.dir, appFileName), core.AppIndexFile{
		Version:   core.IndexVersion,
		UpdatedAt: time.Now(),
		Entries:   entries,
	})
}

// LoadFolderIndex returns the cached folder index (empty on any degradation).
func (c *Cache) LoadFolderIndex() (core.FolderIndexFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return readJSON[core.FolderIndexFile](filepath.Join(c.dir, folderFileName))
}

// ReplaceFolderEntriesForSource swaps the given source's entries in the
// in-memory mirror and atomically rewrites folder-index.json. Other
// sources' entries are untouched.
func (c *Cache) ReplaceFolderEntriesForSource(sourceID string, entries []core.FolderIndexEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	replaced := make([]core.FolderIndexEntry, 0, len(c.folderEntries))
	for _, e := range c.folderEntries {
		if e.SourceID != sourceID {
			replaced = append(replaced, e)
		}
	}
	replaced = append(replaced, entries...)
	c.folderEntries = replaced
	return writeJSON(filepath.Join(c.dir, folderFileName), core.FolderIndexFile{
		Version:   core.IndexVersion,
		UpdatedAt: time.Now(),
		Entries:   replaced,
	})
}

// DeleteFolderEntriesForSource removes the source's entries from the mirror
// and the file. Unknown sources are a no-op.
func (c *Cache) DeleteFolderEntriesForSource(sourceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make([]core.FolderIndexEntry, 0, len(c.folderEntries))
	for _, e := range c.folderEntries {
		if e.SourceID != sourceID {
			kept = append(kept, e)
		}
	}
	c.folderEntries = kept
	return writeJSON(filepath.Join(c.dir, folderFileName), core.FolderIndexFile{
		Version:   core.IndexVersion,
		UpdatedAt: time.Now(),
		Entries:   kept,
	})
}

// FolderEntriesForSource returns the mirrored entries of one source without
// touching the filesystem (used for fast re-enable).
func (c *Cache) FolderEntriesForSource(sourceID string) []core.FolderIndexEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []core.FolderIndexEntry
	for _, e := range c.folderEntries {
		if e.SourceID == sourceID {
			out = append(out, e)
		}
	}
	return out
}

// readJSON decodes one cache file. Missing files yield the zero value; so
// do corrupt JSON and future versions (logged, never an error).
func readJSON[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, nil
		}
		return zero, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var peek struct {
		Version int
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		log.Printf("indexcache: %s corrupt (%v); rebuilding", filepath.Base(path), err)
		return zero, nil
	}
	if peek.Version > core.IndexVersion {
		log.Printf("indexcache: %s has future version %d; rebuilding", filepath.Base(path), peek.Version)
		return zero, nil
	}
	var f T
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("indexcache: %s corrupt (%v); rebuilding", filepath.Base(path), err)
		return zero, nil
	}
	return f, nil
}

// writeJSON atomically replaces path: marshal to path+".tmp", then rename.
func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(tmp), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", filepath.Base(path), err)
	}
	return nil
}
