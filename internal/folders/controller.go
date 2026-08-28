package folders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kyvro/internal/core"
	"kyvro/internal/indexcache"
)

// Controller owns the folder-search lifecycle: folder-source configuration
// (bbolt), the rebuildable index cache (JSON) and the in-memory provider.
// Business logic lives here, not in the Wails service.
//
// Ordering contract: mutations write the cache file first, then swap the
// provider; locks are never held in reverse order.
type Controller struct {
	store *core.Store
	cache *indexcache.Cache
	prov  *Provider
	now   func() time.Time

	mu     sync.Mutex
	status map[string]SourceStatus
}

// SourceStatus is the mutable scan state of one source.
type SourceStatus struct {
	LastScannedAt time.Time
	LastError     string
	Scanning      bool
}

// NewController creates a controller over the store and cache.
func NewController(store *core.Store, cache *indexcache.Cache) *Controller {
	return &Controller{
		store:  store,
		cache:  cache,
		prov:   NewProvider(nil),
		now:    time.Now,
		status: make(map[string]SourceStatus),
	}
}

// Provider returns the folder search provider for the engine.
func (c *Controller) Provider() *Provider { return c.prov }

// LoadAtStartup seeds the provider from persisted state: folder sources
// from bbolt, index entries from cache/folder-index.json filtered to
// enabled sources. It never scans; refreshes happen in the background.
// A nil cache seeds an empty provider (sources stay configured).
func (c *Controller) LoadAtStartup() error {
	sources, err := c.store.ListFolderSources()
	if err != nil {
		return fmt.Errorf("folders: list sources: %w", err)
	}
	if c.cache == nil {
		return nil
	}
	indexFile, err := c.cache.LoadFolderIndex()
	if err != nil {
		return fmt.Errorf("folders: load index cache: %w", err)
	}
	enabled := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		if s.Enabled {
			enabled[s.ID] = struct{}{}
		}
	}
	var entries []core.FolderIndexEntry
	for _, e := range indexFile.Entries {
		if _, ok := enabled[e.SourceID]; ok {
			entries = append(entries, e)
		}
	}
	c.prov.Replace(entries)
	return nil
}

// AddSource validates and scans a new root synchronously (spec §10.3) so
// the folder is searchable the moment the call returns.
func (c *Controller) AddSource(ctx context.Context, path string, maxDepth int) (core.FolderSource, error) {
	if maxDepth < 1 {
		return core.FolderSource{}, fmt.Errorf("folders: max depth must be >= 1, got %d", maxDepth)
	}
	abs, err := expandRoot(path)
	if err != nil {
		return core.FolderSource{}, err
	}

	c.mu.Lock()
	dup := c.duplicatePathLocked(abs)
	c.mu.Unlock()
	if dup {
		return core.FolderSource{}, fmt.Errorf("folders: %s is already a source", abs)
	}

	id, err := newSourceID()
	if err != nil {
		return core.FolderSource{}, err
	}
	now := c.now()
	src := core.FolderSource{
		ID:        id,
		Path:      abs,
		MaxDepth:  maxDepth,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	c.mu.Lock()
	c.status[id] = SourceStatus{Scanning: true}
	c.mu.Unlock()

	entries, scanErr := Scan(ctx, src, c.now())

	if err := c.store.PutFolderSource(src); err != nil {
		c.setStatus(id, SourceStatus{LastError: err.Error()})
		return core.FolderSource{}, fmt.Errorf("folders: save source: %w", err)
	}
	if scanErr != nil {
		c.setStatus(id, SourceStatus{LastError: scanErr.Error()})
		return src, scanErr
	}
	if err := c.cache.ReplaceFolderEntriesForSource(id, entries); err != nil {
		log.Printf("folders: update index cache for %s: %v", abs, err)
	}
	c.prov.ReplaceSourceEntries(id, entries)
	c.setStatus(id, SourceStatus{LastScannedAt: c.now()})
	return src, nil
}

// RemoveSource deletes the source record, its cache entries and its
// provider entries.
func (c *Controller) RemoveSource(id string) error {
	if err := c.store.DeleteFolderSource(id); err != nil {
		return fmt.Errorf("folders: delete source: %w", err)
	}
	if err := c.cache.DeleteFolderEntriesForSource(id); err != nil {
		log.Printf("folders: delete index cache for %s: %v", id, err)
	}
	c.prov.DeleteSourceEntries(id)
	c.mu.Lock()
	delete(c.status, id)
	c.mu.Unlock()
	return nil
}

// SetEnabled toggles a source. Disabling keeps the cache entries (fast
// re-enable) but removes them from the provider; enabling restores the
// cached entries immediately and refreshes in the background.
func (c *Controller) SetEnabled(ctx context.Context, id string, enabled bool) error {
	src, err := c.store.GetFolderSource(id)
	if err != nil {
		return err
	}
	src.Enabled = enabled
	src.UpdatedAt = c.now()
	if err := c.store.PutFolderSource(src); err != nil {
		return fmt.Errorf("folders: save source: %w", err)
	}

	if enabled {
		c.prov.ReplaceSourceEntries(id, c.cache.FolderEntriesForSource(id))
		go func() {
			if err := c.RefreshSource(ctx, id); err != nil {
				log.Printf("folders: background refresh %s: %v", src.Path, err)
			}
		}()
	} else {
		c.prov.DeleteSourceEntries(id)
	}
	return nil
}

// RefreshSource rescans one source. Disabled sources return an explicit
// error; scan failures keep the existing cache and provider entries and
// are recorded in the source status.
func (c *Controller) RefreshSource(ctx context.Context, id string) error {
	src, err := c.store.GetFolderSource(id)
	if err != nil {
		return err
	}
	if !src.Enabled {
		return fmt.Errorf("folders: source %s is disabled", src.Path)
	}

	c.mu.Lock()
	st := c.status[id]
	st.Scanning = true
	c.status[id] = st
	c.mu.Unlock()

	entries, scanErr := Scan(ctx, src, c.now())

	c.mu.Lock()
	st = c.status[id]
	st.Scanning = false
	if scanErr != nil {
		st.LastError = scanErr.Error()
		c.status[id] = st
		c.mu.Unlock()
		return scanErr
	}
	st.LastScannedAt = c.now()
	st.LastError = ""
	c.status[id] = st
	c.mu.Unlock()

	if err := c.cache.ReplaceFolderEntriesForSource(id, entries); err != nil {
		log.Printf("folders: update index cache for %s: %v", src.Path, err)
	}
	c.prov.ReplaceSourceEntries(id, entries)
	return nil
}

// RefreshAll rescans every enabled source; each failure is recorded and
// returned as the aggregate error but never aborts the other sources.
func (c *Controller) RefreshAll(ctx context.Context) error {
	sources, err := c.store.ListFolderSources()
	if err != nil {
		return err
	}
	var firstErr error
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		if err := c.RefreshSource(ctx, src.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Sources returns the settings view of every source with live status.
func (c *Controller) Sources() []core.FolderSourceInfo {
	sources, err := c.store.ListFolderSources()
	if err != nil {
		log.Printf("folders: list sources: %v", err)
		return nil
	}
	counts := c.prov.CountBySource()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]core.FolderSourceInfo, 0, len(sources))
	for _, s := range sources {
		st := c.status[s.ID]
		count := counts[s.ID]
		if !s.Enabled {
			// Disabled sources keep their cached entries; report those.
			count = len(c.cache.FolderEntriesForSource(s.ID))
		}
		out = append(out, core.FolderSourceInfo{
			Source:        s,
			DisplayPath:   AbbrevHome(s.Path),
			IndexedCount:  count,
			LastScannedAt: st.LastScannedAt,
			LastScanError: st.LastError,
			Scanning:      st.Scanning,
		})
	}
	return out
}

// BackgroundRefresh rescans all enabled sources; safe to run in a
// goroutine at startup.
func (c *Controller) BackgroundRefresh(ctx context.Context) {
	if err := c.RefreshAll(ctx); err != nil {
		log.Printf("folders: startup refresh: %v", err)
	}
}

// duplicatePathLocked reports whether abs is already configured as a
// source. Callers hold c.mu.
func (c *Controller) duplicatePathLocked(abs string) bool {
	sources, err := c.store.ListFolderSources()
	if err != nil {
		return false
	}
	for _, s := range sources {
		if s.Path == abs {
			return true
		}
	}
	return false
}

func (c *Controller) setStatus(id string, st SourceStatus) {
	c.mu.Lock()
	c.status[id] = st
	c.mu.Unlock()
}

// expandRoot expands ~, cleans and absolutises path, and verifies it is an
// existing directory.
func expandRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("folders: empty path")
	}
	if path == "~" || len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("folders: resolve home: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("folders: absolutise %s: %w", path, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("folders: stat %s: %w", abs, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("folders: %s is not a directory", abs)
	}
	return abs, nil
}

// newSourceID returns a random 8-byte hex ID.
func newSourceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("folders: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
