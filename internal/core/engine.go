package core

import (
	"context"
	"sort"
	"sync"
	"time"
)

// DefaultLimit is the maximum number of results returned per search.
const DefaultLimit = 9

// Engine aggregates providers, applies frecency ranking and truncates.
// Providers are consulted in order: results from provider i always rank
// below results from provider i-1, regardless of score, so later
// providers act as fallbacks (the web search entry stays at the tail).
type Engine struct {
	providers []Provider
	store     *Store
	limit     int

	mu            sync.RWMutex
	usageSnapshot map[string]Usage

	// now is injectable for deterministic tests.
	now func() time.Time
}

// NewEngine creates an engine over the given providers (in priority order)
// backed by the usage store. limit <= 0 falls back to DefaultLimit.
func NewEngine(providers []Provider, store *Store, limit int) *Engine {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Engine{
		providers:     providers,
		store:         store,
		limit:         limit,
		usageSnapshot: make(map[string]Usage),
		now:           time.Now,
	}
}

// SetNow overrides the clock (tests only).
func (e *Engine) SetNow(f func() time.Time) { e.now = f }

// refresh loads the usage snapshot from the store.
func (e *Engine) refresh() error {
	all, err := e.store.All()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.usageSnapshot = all
	e.mu.Unlock()
	return nil
}

// usage returns the snapshot entry for id.
func (e *Engine) usage(id string) Usage {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.usageSnapshot[id]
}

// Search queries every provider, boosts scores with frecency, and returns
// at most limit results. With an empty query it surfaces the provider
// defaults ordered purely by frecency (the apps provider returns its full
// list with score 0 in that case).
func (e *Engine) Search(ctx context.Context, query string) []SearchResult {
	if err := e.refresh(); err != nil {
		// Ranking degrades to fuzzy-only; the caller decides whether to log.
		_ = err
	}
	now := e.now()

	var out []SearchResult
	remaining := e.limit
	for _, p := range e.providers {
		if remaining <= 0 {
			break
		}
		results := p.Search(ctx, query)
		if len(results) == 0 {
			continue
		}
		for i := range results {
			u := e.usage(results[i].ID)
			results[i].Score += Frecency(u.Count, u.LastUsed, now)
		}
		sortResults(results)
		if len(results) > remaining {
			results = results[:remaining]
		}
		out = append(out, results...)
		remaining -= len(results)
	}
	return out
}

// sortResults sorts by score descending, breaking ties by title so the
// empty-query default list is stable.
func sortResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Title < results[j].Title
	})
}
