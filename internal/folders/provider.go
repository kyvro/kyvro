package folders

import (
	"context"
	"os"
	"strings"
	"sync"

	"kyvro/internal/core"
)

// homeDir is resolved once for the ~-abbreviated subtitle (display only).
var homeDir = func() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}()

// Provider searches the in-memory folder index. The index (entries +
// prebuilt search keys) is swapped atomically on Replace*; Search never
// allocates key slices and never touches the filesystem.
type Provider struct {
	mu    sync.RWMutex
	index *core.SearchIndex[core.FolderIndexEntry]
}

// NewProvider builds a provider over the given entries (filtered by the
// caller to enabled sources).
func NewProvider(entries []core.FolderIndexEntry) *Provider {
	p := &Provider{}
	p.Replace(entries)
	return p
}

// ID implements core.Provider.
func (p *Provider) ID() string { return "folders" }

// Replace atomically swaps the whole index.
func (p *Provider) Replace(entries []core.FolderIndexEntry) {
	ix := core.BuildSearchIndex(entries, func(e core.FolderIndexEntry) []string {
		return e.SearchKeys
	})
	p.mu.Lock()
	p.index = ix
	p.mu.Unlock()
}

// ReplaceSourceEntries swaps only the given source's entries, keeping all
// others in place.
func (p *Provider) ReplaceSourceEntries(sourceID string, entries []core.FolderIndexEntry) {
	p.mu.RLock()
	var kept []core.FolderIndexEntry
	if p.index != nil {
		for _, e := range p.index.Items {
			if e.SourceID != sourceID {
				kept = append(kept, e)
			}
		}
	}
	p.mu.RUnlock()
	kept = append(kept, entries...)
	p.Replace(kept)
}

// DeleteSourceEntries removes the source's entries from the index.
func (p *Provider) DeleteSourceEntries(sourceID string) {
	p.ReplaceSourceEntries(sourceID, nil)
}

// EntriesForSource returns a copy of the source's current entries.
func (p *Provider) EntriesForSource(sourceID string) []core.FolderIndexEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.index == nil {
		return nil
	}
	var out []core.FolderIndexEntry
	for _, e := range p.index.Items {
		if e.SourceID == sourceID {
			out = append(out, e)
		}
	}
	return out
}

// CountBySource reports the indexed entry count per source ID.
func (p *Provider) CountBySource() map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]int)
	if p.index != nil {
		for _, e := range p.index.Items {
			out[e.SourceID]++
		}
	}
	return out
}

// Search implements core.Provider. An empty query returns every indexed
// entry with score 0 (the engine orders by frecency); otherwise it fuzzy
// matches over the prebuilt keys, which contain only folder basenames.
func (p *Provider) Search(_ context.Context, query string) []core.SearchResult {
	p.mu.RLock()
	ix := p.index
	p.mu.RUnlock()
	if ix == nil || ix.Len() == 0 {
		return nil
	}
	if query == "" {
		results := make([]core.SearchResult, 0, ix.Len())
		for _, e := range ix.Items {
			results = append(results, entryToResult(e, 0))
		}
		return results
	}
	hits := ix.Find(query)
	if len(hits) == 0 {
		return nil
	}
	results := make([]core.SearchResult, 0, len(hits))
	for _, h := range hits {
		results = append(results, entryToResult(h.Item, h.Score))
	}
	return results
}

// entryToResult shapes one row exactly as spec §7: primary open action,
// reveal/copy secondary actions, Data carries the absolute path.
func entryToResult(e core.FolderIndexEntry, score float64) core.SearchResult {
	return core.SearchResult{
		ID:       e.ID,
		Kind:     core.KindFolder,
		Title:    e.Name,
		Subtitle: abbrevHome(e.Path),
		Data: map[string]any{
			"path": e.Path,
		},
		PrimaryAction: core.Action{Kind: core.ActionOpenPath, Arg: e.Path},
		Actions: []core.ActionItem{
			{
				ID:       "reveal",
				Title:    "Reveal in Finder",
				Shortcut: "cmd+enter",
				Action:   core.Action{Kind: core.ActionRevealPath, Arg: e.Path},
			},
			{
				ID:       "copy-path",
				Title:    "Copy Path",
				Shortcut: "cmd+c",
				Action:   core.Action{Kind: core.ActionCopyText, Arg: e.Path},
			},
		},
		Score: score,
	}
}

// AbbrevHome shortens an absolute path for display by replacing the home
// prefix with ~ — shared by search-row subtitles and the settings UI so
// usernames never appear on screen.
func AbbrevHome(path string) string { return abbrevHome(path) }

// abbrevHome shortens the display path by replacing the home prefix with ~.
func abbrevHome(path string) string {
	if homeDir == "" || !strings.HasPrefix(path, homeDir) {
		return path
	}
	rest := path[len(homeDir):]
	switch {
	case rest == "":
		return "~"
	case rest[0] == '/':
		return "~" + rest
	default:
		// Directory boundary mismatch (e.g. /Users/alicex for /Users/alice).
		return path
	}
}
