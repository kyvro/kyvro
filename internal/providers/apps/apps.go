// Package apps implements the application-search provider backed by a
// platform.AppSource. Fuzzy matching runs over the cached app list; the
// source is rescanned in the background at most once per rescanInterval.
package apps

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sahilm/fuzzy"

	"kyvro/internal/core"
	"kyvro/internal/platform"
)

// rescanInterval throttles background rescans: at most one per minute.
const rescanInterval = 60 * time.Second

// Provider searches installed applications.
type Provider struct {
	source platform.AppSource

	mu          sync.Mutex // guards lastRescan
	lastRescan  time.Time
	scanning    atomic.Bool
	rescanEvery time.Duration

	// now is injectable for tests.
	now func() time.Time
}

// New creates an apps provider over src.
func New(src platform.AppSource) *Provider {
	return &Provider{
		source:      src,
		rescanEvery: rescanInterval,
		now:         time.Now,
	}
}

// ID implements core.Provider.
func (p *Provider) ID() string { return "apps" }

// Search implements core.Provider. An empty query returns every known app
// with score 0 — the engine then orders them by frecency.
func (p *Provider) Search(_ context.Context, query string) []core.SearchResult {
	p.maybeRescan()

	entries := p.source.List()
	if len(entries) == 0 {
		return nil
	}
	if query == "" {
		results := make([]core.SearchResult, 0, len(entries))
		for _, e := range entries {
			results = append(results, entryToResult(e, 0))
		}
		return results
	}

	// Flatten per-app search keys (name, alt names, pinyin) into one
	// fuzzy pass, then keep each app's best score across its keys.
	keys := make([]string, 0, len(entries)*3)
	appOf := make([]int, 0, len(entries)*3)
	for i := range entries {
		for _, k := range searchKeys(entries[i]) {
			keys = append(keys, k)
			appOf = append(appOf, i)
		}
	}
	matches := fuzzy.Find(query, keys)
	best := make([]float64, len(entries))
	for i := range best {
		best[i] = -1
	}
	for _, m := range matches {
		if float64(m.Score) > best[appOf[m.Index]] {
			best[appOf[m.Index]] = float64(m.Score)
		}
	}
	results := make([]core.SearchResult, 0, len(matches))
	for i, s := range best {
		if s >= 0 {
			results = append(results, entryToResult(entries[i], s))
		}
	}
	return results
}

// Warmup performs the initial full scan (call once at startup, ideally in
// a goroutine) and marks the cache fresh.
func (p *Provider) Warmup() {
	if err := p.source.Rescan(); err != nil {
		log.Printf("apps: initial scan: %v", err)
	}
	p.mu.Lock()
	p.lastRescan = time.Now()
	p.mu.Unlock()
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
		if err := p.source.Rescan(); err != nil {
			log.Printf("apps: rescan: %v", err)
		}
		p.mu.Lock()
		p.lastRescan = time.Now()
		p.mu.Unlock()
	}()
}

func entryToResult(e core.AppEntry, score float64) core.SearchResult {
	return core.SearchResult{
		ID:       e.ID,
		Title:    e.Name,
		Subtitle: e.Path,
		Action: core.Action{
			Kind: core.ActionLaunchApp,
			Arg:  e.Path,
		},
		Score:    score,
		IconPath: e.IconPath,
	}
}
