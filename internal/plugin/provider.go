package plugin

import (
	"context"
	"time"

	"kyvro/internal/core"
)

// commandRef locates the plugin entry a prefix triggers.
type commandRef struct {
	pluginID  string
	commandID string
}

// commandIndex is the in-memory prefix index over enabled plugins' manifest
// commands (index.md §1.3): prefixes are lowercased, values are
// (pluginID, commandID), and a hit is one map lookup — the matching path
// never touches JS. Purely runtime state: rebuilt on plugin lifecycle
// events, never persisted.
type commandIndex struct {
	byPrefix map[string]commandRef
	maxLen   int // longest registered prefix; bounds match's loop
}

// match returns the longest registered prefix anchored at the start of the
// (already lowercased) query: the query must equal the prefix or continue
// with a space, so ordinary words merely starting with a prefix (e.g.
// "ghost" for "gh") never hit. Each candidate length costs one map lookup;
// the loop is bounded by the longest registered prefix, so matching is
// O(1) in practice regardless of plugin count.
func (idx commandIndex) match(q string) (commandRef, bool) {
	if idx.byPrefix == nil {
		return commandRef{}, false
	}
	for l := min(len(q), idx.maxLen); l > 0; l-- {
		if ref, ok := idx.byPrefix[q[:l]]; ok {
			if l == len(q) || q[l] == ' ' {
				return ref, true
			}
		}
	}
	return commandRef{}, false
}

// PluginProvider aggregates all loaded plugins behind one core.Provider so
// the engine keeps its fixed priority order (plugins sit between folders
// and the web fallback). Search is prefix-gated: only a command-index hit
// live-calls the owning plugin's onAction; misses return without touching
// any VM.
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
// lists are the apps provider's job. Otherwise the query is matched against
// the command index; a hit live-calls that plugin's onAction under the soft
// timeout and its rows join the main list.
func (p *PluginProvider) Search(ctx context.Context, query string) []core.SearchResult {
	if query == "" {
		return nil
	}
	res, _ := p.mgr.liveSearch(ctx, query, p.searchTimeout)
	return res
}
