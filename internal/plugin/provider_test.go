package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"kyvro/internal/core"
)

func TestProviderPrefixGate(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "basic.js"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()
	p := m.Provider()

	results := p.Search(context.Background(), "b64 hello")
	if len(results) != 1 || results[0].ID != "plugin:com.example.test:first" {
		t.Fatalf("prefix hit must return live results: %+v", results)
	}
	// Misses: wrong prefix, longer words sharing the prefix (no word
	// boundary), and the empty query.
	for _, q := range []string{"xx hello", "ghost", "b64x hello", ""} {
		if results := p.Search(context.Background(), q); len(results) != 0 {
			t.Fatalf("query %q must not invoke the plugin: %+v", q, results)
		}
	}
}

func TestProviderExactPrefixHit(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "basic.js"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "b64")
	if len(results) != 1 || results[0].Title != "P:b64" {
		t.Fatalf("bare prefix must hit the index: %+v", results)
	}
}

// leakJS returns a plugin whose onAction answers every call with one LEAK
// row — any LEAK row in a search proves the plugin was invoked.
func leakJS() string {
	return `module.exports = {onAction:function(){ return [{id:"leak",title:"LEAK",actions:[{type:"copy",value:"x"}]}] }}`
}

func TestProviderNoCommandsNeverSearched(t *testing.T) {
	root := t.TempDir()
	// The JS would return a result for any call; without commands the
	// plugin never joins the command index and must never be invoked.
	mm := manifestMap(t, validManifest)
	delete(mm, "commands")
	writePluginDir(t, root, "com.example.test", "0.1.0", string(marshal(t, mm)), leakJS())
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	for _, q := range []string{"anything", "b64 x", "test"} {
		for _, r := range m.Provider().Search(context.Background(), q) {
			if r.Title == "LEAK" {
				t.Fatalf("plugin without commands was searched (q=%q)", q)
			}
		}
	}
}

// prefixManifestFor builds a minimal one-command manifest with the given
// trigger prefix.
func prefixManifestFor(id, prefix string) string {
	return fmt.Sprintf(`{"schemaVersion":1,"id":%q,"version":"0.1.0","main":"index.js","minHostVersion":"0.1.0","commands":[{"id":"cmd","prefix":%q}]}`, id, prefix)
}

// echoJS returns a plugin that titles its row with its own plugin id and
// the full query, so tests can see which plugin answered.
func echoJS(id string) string {
	return fmt.Sprintf(`module.exports = {onAction:function(i,a){ return [{id:"r",title:%q+":"+(a&&a[0]||""),actions:[{type:"copy",value:"v"}]}] }}`, id)
}

func TestProviderLongestPrefixWins(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.a", "0.1.0", prefixManifestFor("com.example.a", "gh"), echoJS("a"))
	writePluginDir(t, root, "com.example.b", "0.1.0", prefixManifestFor("com.example.b", "ghs"), echoJS("b"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "ghs x")
	if len(results) != 1 || results[0].ID != "plugin:com.example.b:r" {
		t.Fatalf("longest prefix must win: %+v", results)
	}
	results = m.Provider().Search(context.Background(), "gh x")
	if len(results) != 1 || results[0].ID != "plugin:com.example.a:r" {
		t.Fatalf("shorter prefix must serve its own query: %+v", results)
	}
}

func TestProviderPrefixConflictKeepsFirst(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.a", "0.1.0", prefixManifestFor("com.example.a", "dup"), echoJS("a"))
	writePluginDir(t, root, "com.example.b", "0.1.0", prefixManifestFor("com.example.b", "dup"), echoJS("b"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "dup x")
	if len(results) != 1 || results[0].ID != "plugin:com.example.a:r" {
		t.Fatalf("conflict must resolve to the lowest plugin id: %+v", results)
	}
}

func TestProviderDisableRemovesPrefix(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "basic.js"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	if err := m.SetEnabled("com.example.test", false); err != nil {
		t.Fatal(err)
	}
	if got := m.Provider().Search(context.Background(), "b64 x"); len(got) != 0 {
		t.Fatalf("disabled plugin must leave the index: %+v", got)
	}
	if err := m.SetEnabled("com.example.test", true); err != nil {
		t.Fatal(err)
	}
	if got := m.Provider().Search(context.Background(), "b64 x"); len(got) != 1 {
		t.Fatalf("re-enabling must restore the index: %+v", got)
	}
}

func TestProviderThreeStrikesDisables(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "spin.js"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()
	p := m.Provider()
	p.searchTimeout = 60 * time.Millisecond

	for i := 0; i < 3; i++ {
		if got := p.Search(context.Background(), "b64 x"); len(got) != 0 {
			t.Fatalf("timed-out search must return no results (round %d): %+v", i, got)
		}
	}
	m.mu.RLock()
	disabled := m.plugins["com.example.test"].disabled
	m.mu.RUnlock()
	if !disabled {
		t.Fatal("plugin must be disabled after 3 consecutive timeouts")
	}
	// Removed from the index and refused actions; flagged as auto-disabled.
	if got := p.Search(context.Background(), "b64 x"); len(got) != 0 {
		t.Fatalf("disabled plugin still searched: %+v", got)
	}
	if _, err := m.RunAction(context.Background(), "com.example.test", "test.cmd", nil); err == nil {
		t.Fatal("RunAction must fail for a disabled plugin")
	}
	for _, info := range m.ListPlugins() {
		if info.ID != "com.example.test" {
			continue
		}
		if !info.Disabled || !info.AutoDisabled {
			t.Fatalf("want auto-disabled flag, got %+v", info)
		}
	}
	// Re-enabling clears strikes and restores the index. (The fixture still
	// spins forever, so observability here is the strike reset plus the
	// enabled flag.)
	if err := m.SetEnabled("com.example.test", true); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	enabled := !m.plugins["com.example.test"].disabled
	strikes := m.plugins["com.example.test"].rt.Strikes()
	m.mu.RUnlock()
	if !enabled || strikes != 0 {
		t.Fatalf("re-enable: enabled=%v strikes=%d", enabled, strikes)
	}
}

// fakeProvider feeds fixed results into engine-ordering tests.
type fakeProvider struct {
	id      string
	results []core.SearchResult
}

func (f *fakeProvider) ID() string { return f.id }

func (f *fakeProvider) Search(_ context.Context, _ string) []core.SearchResult {
	out := make([]core.SearchResult, len(f.results))
	copy(out, f.results)
	return out
}

func TestEngineIntegrationPluginOrder(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "basic.js"))
	m := NewManager(root, testStore(t), nil)
	m.LoadAll()
	defer m.Shutdown()

	apps := &fakeProvider{id: "apps", results: []core.SearchResult{{ID: "app:1", Title: "App", Score: 10}}}
	web := &fakeProvider{id: "web", results: []core.SearchResult{{ID: "web:q", Title: "Search Google", Score: 1}}}
	engine := core.NewEngine([]core.Provider{apps, m.Provider(), web}, testStore(t), 0)

	results := engine.Search(context.Background(), "b64 hello")
	if len(results) != 3 {
		t.Fatalf("want app+plugin+web rows, got %+v", results)
	}
	if results[0].ID != "app:1" {
		t.Errorf("apps must outrank plugins, got %q first", results[0].ID)
	}
	if results[1].ID != "plugin:com.example.test:first" {
		t.Errorf("plugin row misplaced: %+v", results)
	}
	if results[2].ID != "web:q" {
		t.Errorf("web fallback must stay last, got %+v", results)
	}
	if strings.HasPrefix(results[1].ID, "plugin:com.example.test:cmd:") {
		t.Errorf("command rows must come from live calls, not surfacing: %+v", results[1])
	}
}
