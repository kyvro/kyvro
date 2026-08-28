// Package apps implements the application-search provider backed by a
// platform.AppSource. Searches run over a prebuilt fuzzy index; the source
// is rescanned in the background at most once per rescanInterval, and every
// successful rescan atomically swaps the provider snapshot and (via the
// cache hook) rewrites cache/app-index.json outside all locks.
package apps

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"kyvro/internal/core"
	"kyvro/internal/platform"
)

// rescanInterval throttles background rescans: at most one per minute.
const rescanInterval = 60 * time.Second

// Provider searches installed applications.
type Provider struct {
	source platform.AppSource

	// cacheHook, when set, is invoked with the freshly scanned app list
	// after a successful rescan — outside every provider lock, so the hook
	// may do blocking JSON I/O.
	cacheHook func([]core.AppEntry)

	// idxMu guards the entries+index pair, which is always swapped as a
	// unit. It is never held while taking mu, and vice versa.
	idxMu   sync.RWMutex
	entries []core.AppEntry
	index   *core.SearchIndex[core.AppEntry]

	mu          sync.Mutex // guards lastRescan
	lastRescan  time.Time
	scanning    atomic.Bool
	rescanEvery time.Duration

	// now is injectable for tests.
	now func() time.Time
}

// New creates an apps provider over src, seeding synchronously from the
// source's current cache so the first search has data (existing tests rely
// on this).
func New(src platform.AppSource) *Provider {
	p := &Provider{
		source:      src,
		rescanEvery: rescanInterval,
		now:         time.Now,
	}
	p.swap(src.List())
	return p
}

// NewWithCache creates a provider seeded from persisted cache entries; the
// platform source is only consulted by the background rescan (Warmup), so
// startup search works without an initial disk scan.
func NewWithCache(src platform.AppSource, seed []core.AppIndexEntry) *Provider {
	p := &Provider{
		source:      src,
		rescanEvery: rescanInterval,
		now:         time.Now,
	}
	// Rebuild AppEntry values from the cache; search keys are recomputed
	// deterministically from Name/AltNames.
	entries := make([]core.AppEntry, 0, len(seed))
	for _, e := range seed {
		entries = append(entries, core.AppEntry{
			ID:       e.ID,
			Name:     e.Name,
			Path:     e.Path,
			BundleID: e.BundleID,
			IconPath: e.IconPath,
			AltNames: e.AltNames,
		})
	}
	p.swap(entries)
	return p
}

// SetCacheHook registers h, called after each successful rescan with the
// new full app list. Must be called before Warmup.
func (p *Provider) SetCacheHook(hook func([]core.AppEntry)) {
	p.idxMu.Lock()
	p.cacheHook = hook
	p.idxMu.Unlock()
}

// ID implements core.Provider.
func (p *Provider) ID() string { return "apps" }

// Search implements core.Provider. An empty query returns every known app
// with score 0 — the engine then orders them by frecency. Matching runs
// over the prebuilt index; no keys are rebuilt here.
func (p *Provider) Search(_ context.Context, query string) []core.SearchResult {
	p.maybeRescan()

	p.idxMu.RLock()
	entries, index := p.entries, p.index
	p.idxMu.RUnlock()
	if index == nil || index.Len() == 0 {
		return nil
	}
	if query == "" {
		results := make([]core.SearchResult, 0, index.Len())
		for _, e := range entries {
			results = append(results, entryToResult(e, 0))
		}
		return results
	}
	hits := index.Find(query)
	if len(hits) == 0 {
		return nil
	}
	results := make([]core.SearchResult, 0, len(hits))
	for _, h := range hits {
		results = append(results, entryToResult(h.Item, h.Score))
	}
	return results
}

// Warmup performs the initial full scan (call once at startup, ideally in
// a goroutine) and marks the cache fresh.
func (p *Provider) Warmup() {
	p.rescan("warmup")
}

// maybeRescan triggers a background rescan when the cache is older than
// rescanInterval. It never blocks the current search.
func (p *Provider) maybeRescan() {
	p.mu.Lock()
	stale := p.now().Sub(p.lastRescan) >= p.rescanEvery
	p.mu.Unlock()
	if !stale {
		return
	}
	// Swap-and-check: only one in-flight scan at a time.
	if !p.scanning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer p.scanning.Store(false)
		p.rescan("rescan")
	}()
}

// rescan rescans the source and, on success, swaps the snapshot then
// invokes the cache hook. A failed rescan keeps the old snapshot.
func (p *Provider) rescan(what string) {
	if err := p.source.Rescan(); err != nil {
		log.Printf("apps: %s: %v", what, err)
		p.mu.Lock()
		p.lastRescan = time.Now()
		p.mu.Unlock()
		return
	}
	list := p.source.List()

	p.idxMu.Lock()
	p.swap(list)
	hook := p.cacheHook
	p.idxMu.Unlock()

	p.mu.Lock()
	p.lastRescan = time.Now()
	p.mu.Unlock()

	// Persist outside every lock; hook may block on file I/O.
	if hook != nil {
		hook(list)
	}
}

// swap installs an entries+index pair. Callers hold idxMu (or have not
// published the provider yet).
func (p *Provider) swap(entries []core.AppEntry) {
	p.entries = entries
	p.index = core.BuildSearchIndex(entries, searchKeys)
}

// AppIndexEntries converts a scanned app list to its persisted cache form
// (IDs re-derived, search keys precomputed).
func AppIndexEntries(list []core.AppEntry) []core.AppIndexEntry {
	out := make([]core.AppIndexEntry, 0, len(list))
	for _, e := range list {
		out = append(out, core.AppIndexEntry{
			ID:         appResultID(e),
			Name:       e.Name,
			Path:       e.Path,
			BundleID:   e.BundleID,
			IconPath:   e.IconPath,
			AltNames:   e.AltNames,
			SearchKeys: searchKeys(e),
		})
	}
	return out
}

// appResultID returns the stable result ID: "app:<bundleID>", or
// "app:path:<absolute path>" when no bundle ID exists.
func appResultID(e core.AppEntry) string {
	if e.BundleID != "" {
		return "app:" + e.BundleID
	}
	return "app:path:" + e.Path
}

func entryToResult(e core.AppEntry, score float64) core.SearchResult {
	return core.SearchResult{
		ID:       appResultID(e),
		Kind:     core.KindApp,
		Title:    e.Name,
		Subtitle: e.Path,
		PrimaryAction: core.Action{
			Kind: core.ActionLaunchApp,
			Arg:  e.Path,
		},
		Score:    score,
		IconPath: e.IconPath,
	}
}
