package plugin

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sahilm/fuzzy"

	"kyvro/internal/core"
)

// PluginProvider aggregates all loaded plugins behind one core.Provider so
// the engine keeps its fixed priority order: plugins sit between apps and
// the web fallback. It surfaces manifest commands via fuzzy matching (no JS
// involved) and runs prefix-gated live searches in parallel.
type PluginProvider struct {
	mgr           *Manager
	searchTimeout time.Duration
}

// Provider returns the aggregate provider for the manager.
func (m *Manager) Provider() *PluginProvider {
	return &PluginProvider{mgr: m, searchTimeout: SearchTimeout}
}

// ID implements core.Provider.
func (p *PluginProvider) ID() string { return "plugins" }

// Search implements core.Provider. An empty query returns nil — default
// lists are the apps provider's job. Otherwise:
//
//  1. Manifest commands fuzzily match Title+Keywords and surface without
//     touching JS.
//  2. Plugins exporting provider.search AND declaring a matching
//     onSearchPrefix run in parallel (each under the soft timeout);
//     results merge in plugin-id order.
func (p *PluginProvider) Search(ctx context.Context, query string) []core.SearchResult {
	if query == "" {
		return nil
	}
	plugins := p.mgr.snapshot()

	var out []core.SearchResult
	var live []*loadedPlugin
	for _, lp := range plugins {
		out = append(out, p.commandMatches(lp, query)...)
		if lp.rt.HasProvider() && prefixMatches(lp.manifest.SearchPrefixes, query) {
			live = append(live, lp)
		}
	}
	if len(live) == 0 {
		return out
	}

	type liveRes struct {
		lp      *loadedPlugin
		results []core.SearchResult
	}
	results := make([][]core.SearchResult, len(live))
	var wg sync.WaitGroup
	for i, lp := range live {
		wg.Add(1)
		go func(i int, lp *loadedPlugin) {
			defer wg.Done()
			res, err := lp.rt.Search(ctx, query, p.searchTimeout)
			if err != nil {
				if code, ok := CodeOf(err); !ok || code != ErrTimeout {
					log.Printf("plugin %s: search: %v", lp.manifest.ID, err)
				}
				if code, ok := CodeOf(err); ok && code == ErrTimeout && lp.rt.Strikes() >= disableStrikes {
					p.mgr.disable(lp.manifest.ID)
				}
				return
			}
			results[i] = res
		}(i, lp)
	}
	wg.Wait()

	// Append in plugin order (live is sorted by id already).
	for i := range live {
		out = append(out, results[i]...)
	}
	return out
}

// commandMatches fuzzily matches the query against each command's
// Title+Keywords, mirroring the apps provider's scoring.
func (p *PluginProvider) commandMatches(lp *loadedPlugin, query string) []core.SearchResult {
	if len(lp.manifest.Commands) == 0 {
		return nil
	}
	var out []core.SearchResult
	for _, cmd := range lp.manifest.Commands {
		keys := append([]string{cmd.Title}, cmd.Keywords...)
		matches := fuzzy.Find(query, keys)
		best := -1
		for _, m := range matches {
			if m.Score > best {
				best = m.Score
			}
		}
		if best >= 0 {
			out = append(out, commandResult(lp.manifest, cmd, query, float64(best), lp.rt.iconPath))
		}
	}
	return out
}

// prefixMatches reports whether the query starts with any declared prefix
// (case-insensitive).
func prefixMatches(prefixes []string, query string) bool {
	if len(prefixes) == 0 {
		return false
	}
	q := strings.ToLower(query)
	for _, pfx := range prefixes {
		if strings.HasPrefix(q, pfx) {
			return true
		}
	}
	return false
}
