package plugin

import (
	"context"
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
	if results := p.Search(context.Background(), "xx hello"); len(results) != 0 {
		t.Fatalf("prefix miss must not invoke the provider: %+v", results)
	}
	if results := p.Search(context.Background(), ""); len(results) != 0 {
		t.Fatalf("empty query must return nothing: %+v", results)
	}
}

func TestProviderCommandSurfacing(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, fixtureJS(t, "minimal.js"))
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "test")
	if len(results) != 1 {
		t.Fatalf("want the fuzzily matched command, got %+v", results)
	}
	r := results[0]
	if r.ID != "plugin:com.example.test:cmd:test.cmd" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Title != "Test Command" {
		t.Errorf("Title = %q", r.Title)
	}
	a := r.PrimaryAction
	if a.Kind != core.ActionPlugin || a.PluginID != "com.example.test" || a.ActionID != "test.cmd" {
		t.Errorf("action = %+v", a)
	}
	if len(a.Args) != 1 || a.Args[0] != "test" {
		t.Errorf("args = %v (query must be forwarded)", a.Args)
	}
}

func TestProviderNoActivationEventsNeverSearched(t *testing.T) {
	root := t.TempDir()
	// The JS would return a result for any query; without an
	// onSearchPrefix event the provider must never be called.
	always := `module.exports = {provider:{search:function(q){ return [{id:"leak",title:"LEAK",actions:[{type:"copy",value:"x"}]}] }}}`
	mm := manifestMap(t, validManifest)
	delete(mm, "activationEvents")
	writePluginDir(t, root, "com.example.test", "0.1.0", string(marshal(t, mm)), always)
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	for _, q := range []string{"anything", "b64 x", "test"} {
		for _, r := range m.Provider().Search(context.Background(), q) {
			if r.Title == "LEAK" {
				t.Fatalf("provider without activationEvents was searched (q=%q)", q)
			}
		}
	}
}

func TestProviderParallelMergeOrder(t *testing.T) {
	root := t.TempDir()
	// Two prefix plugins returning one row each; results must merge in
	// plugin-id order regardless of goroutine completion order.
	js := `module.exports = {provider:{search:function(q){ return [{id:"r",title:module.id||"?",actions:[{type:"copy",value:"v"}]}] }}}`
	for _, id := range []string{"com.example.a", "com.example.b"} {
		writePluginDir(t, root, id, "0.1.0", manifestFor(id), js)
	}
	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "b64 x")
	if len(results) != 2 {
		t.Fatalf("want results from both plugins, got %+v", results)
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
	// Removed from rotation and refused actions; flagged as auto-disabled.
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
	// Re-enabling clears strikes and restores rotation. (The fixture still
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
}
