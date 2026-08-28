package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"kyvro/internal/core"
)

func TestManagerLoadAllFaultTolerant(t *testing.T) {
	root := t.TempDir()
	// Healthy plugin.
	writePluginDir(t, root, "com.example.good", "0.1.0", manifestFor("com.example.good"), fixtureJS(t, "basic.js"))
	// Bad manifest (unsupported schema).
	writePluginDir(t, root, "com.example.badschema", "0.1.0",
		`{"schemaVersion": 99, "id": "com.example.badschema", "version": "0.1.0", "main": "index.js", "minHostVersion": "0.1.0"}`,
		fixtureJS(t, "basic.js"))
	// Valid manifest, broken JS.
	writePluginDir(t, root, "com.example.brokenjs", "0.1.0", manifestFor("com.example.brokenjs"), "syntax ((error")
	// Not a plugin directory at all.
	if err := os.MkdirAll(filepath.Join(root, "junk"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(root, testStore(t), nil)
	m.LoadAll()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.plugins) != 1 {
		t.Fatalf("want exactly the good plugin loaded, got %d", len(m.plugins))
	}
	if _, ok := m.plugins["com.example.good"]; !ok {
		t.Fatal("com.example.good missing")
	}
}

func TestManagerRunActionUnknownPlugin(t *testing.T) {
	m := NewManager(t.TempDir(), nil, nil)
	m.LoadAll()
	_, err := m.RunAction(context.Background(), "com.example.none", "cmd", nil)
	if code, ok := CodeOf(err); !ok || code != ErrInvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v", err)
	}
}

func TestManagerCurrentJSONPinSelectsVersion(t *testing.T) {
	root := t.TempDir()
	v1 := `module.exports = {provider:{search:function(q){ if(q.indexOf("b64")===0){return [{id:"r",title:"one",actions:[{type:"copy",value:"1"}]}]} return [] }}}`
	v2 := `module.exports = {provider:{search:function(q){ if(q.indexOf("b64")===0){return [{id:"r",title:"two",actions:[{type:"copy",value:"2"}]}]} return [] }}}`
	writePluginDir(t, root, "com.example.test", "0.1.0", validManifest, v1)
	writePluginDir(t, root, "com.example.test", "0.2.0", validManifest, v2)

	m := NewManager(root, nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	if got := firstTitle(t, m, "b64 x"); got != "two" {
		t.Fatalf("highest version must win, got %q", got)
	}

	// Pin the older version; a fresh manager must honor it.
	if err := os.WriteFile(filepath.Join(root, "com.example.test", CurrentFile),
		[]byte(`{"version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(root, nil, nil)
	m2.LoadAll()
	defer m2.Shutdown()
	if got := firstTitle(t, m2, "b64 x"); got != "one" {
		t.Fatalf("current.json pin must win, got %q", got)
	}
}

func TestManagerShutdownReleasesWorkers(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.a", "0.1.0", manifestFor("com.example.a"), fixtureJS(t, "basic.js"))
	writePluginDir(t, root, "com.example.b", "0.1.0", manifestFor("com.example.b"), fixtureJS(t, "basic.js"))

	m := NewManager(root, nil, nil)
	m.LoadAll()

	before := runtime.NumGoroutine()
	m.Shutdown()
	// Shutdown waits for every worker's done channel, so the count must
	// settle immediately (small slack for runtime bookkeeping).
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if now := runtime.NumGoroutine(); now > before {
		t.Errorf("goroutines after shutdown: %d > %d (leak)", now, before)
	}
	// Shutdown is idempotent.
	m.Shutdown()
}

func TestManagerListAndSetEnabled(t *testing.T) {
	root := t.TempDir()
	writePluginDir(t, root, "com.example.a", "0.1.0", manifestFor("com.example.a"), fixtureJS(t, "basic.js"))
	writePluginDir(t, root, "com.example.b", "0.1.0", manifestFor("com.example.b"), fixtureJS(t, "basic.js"))
	dbPath := filepath.Join(t.TempDir(), "data.db")

	// withManager runs one manager at a time over the shared db (bbolt
	// allows a single writer process handle), mimicking app restarts.
	withManager := func(f func(*Manager)) {
		store, err := core.OpenStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		m := NewManager(root, store, nil)
		m.LoadAll()
		defer store.Close() // runs after Shutdown (LIFO)
		defer m.Shutdown()
		f(m)
	}

	withManager(func(m *Manager) {
		if err := m.SetEnabled("com.example.a", false); err != nil {
			t.Fatal(err)
		}
		if got := firstTitle(t, m, "b64 x"); got != "P:b64 x" {
			t.Fatalf("search should fall through to the enabled plugin: %q", got)
		}
		list := m.ListPlugins()
		if len(list) != 2 || list[0].ID != "com.example.a" || list[1].ID != "com.example.b" {
			t.Fatalf("ListPlugins = %+v", list)
		}
		if !list[0].Disabled || list[0].AutoDisabled {
			t.Errorf("a: disabled=%v auto=%v", list[0].Disabled, list[0].AutoDisabled)
		}
		if list[1].Disabled {
			t.Error("b must stay enabled")
		}
		if err := m.SetEnabled("com.example.none", true); err == nil {
			t.Fatal("unknown plugin must fail")
		} else if code, ok := CodeOf(err); !ok || code != ErrInvalidArgument {
			t.Fatalf("unknown plugin must be INVALID_ARGUMENT, got %v", err)
		}
	})

	// A fresh manager over the same store must load com.example.a disabled…
	withManager(func(m *Manager) {
		list := m.ListPlugins()
		if len(list) != 2 || !list[0].Disabled {
			t.Fatalf("user disable must persist across restarts: %+v", list)
		}
		// …and re-enabling clears the persisted state.
		if err := m.SetEnabled("com.example.a", true); err != nil {
			t.Fatal(err)
		}
	})

	withManager(func(m *Manager) {
		if list := m.ListPlugins(); list[0].Disabled {
			t.Fatal("re-enable must be persisted")
		}
	})
}

// firstTitle returns the first plugin-provider result title for query.
func firstTitle(t *testing.T, m *Manager, query string) string {
	t.Helper()
	results := m.Provider().Search(context.Background(), query)
	if len(results) == 0 {
		t.Fatalf("no results for %q", query)
	}
	return results[0].Title
}

// copyPlugin installs the plugin at srcDir into a spec §11 layout under a
// scratch root, using the manifest's own version as the directory name.
func copyPlugin(t *testing.T, srcDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(srcDir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("parse %s: %v", srcDir, err)
	}
	root := t.TempDir()
	dst := filepath.Join(root, manifest.ID, manifest.Version)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestExamplePluginIsValid keeps the shipped example installable: it must
// load through the real manager and answer both extension points.
func TestExamplePluginIsValid(t *testing.T) {
	src := filepath.Join("..", "..", "plugins-example", "com.example.encode")
	if _, err := os.Stat(src); err != nil {
		t.Skip("example plugin not present")
	}
	m := NewManager(copyPlugin(t, src), testStore(t), nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "b64 hello")
	if len(results) == 0 {
		t.Fatal("example plugin did not answer b64 search")
	}
	// Valid base64 input must also produce a decode row ("hello").
	decoded := false
	for _, r := range m.Provider().Search(context.Background(), "b64 aGVsbG8=") {
		if r.Title == "hello" {
			decoded = true
		}
	}
	if !decoded {
		t.Fatal("example plugin did not decode valid base64")
	}
	cmds := m.Provider().Search(context.Background(), "url")
	found := false
	for _, r := range cmds {
		if r.PrimaryAction.Kind == core.ActionPlugin && r.PrimaryAction.ActionID == "encode.url" {
			found = true
		}
	}
	if !found {
		t.Fatal("example command did not surface for 'url'")
	}
	secondary, err := m.RunAction(context.Background(), "com.example.encode", "encode.url", []string{"url"})
	if err != nil || len(secondary) == 0 {
		t.Fatalf("example onAction failed: %+v %v", secondary, err)
	}
}

// TestOfficialGhPluginIsValid keeps the shipped official plugin installable:
// search rows, owner/repo shortcuts and the gh-word fall-through.
func TestOfficialGhPluginIsValid(t *testing.T) {
	src := filepath.Join("..", "..", "plugins-official", "com.kyvro.github")
	if _, err := os.Stat(src); err != nil {
		t.Skip("official plugin not present")
	}
	m := NewManager(copyPlugin(t, src), nil, nil)
	m.LoadAll()
	defer m.Shutdown()

	results := m.Provider().Search(context.Background(), "gh wails")
	if len(results) != 1 {
		t.Fatalf("want exactly the search row, got %+v", results)
	}
	if results[0].PrimaryAction.Kind != core.ActionOpenURL ||
		!strings.Contains(results[0].PrimaryAction.Arg, "github.com/search?q=wails") {
		t.Fatalf("search row action = %+v", results[0].PrimaryAction)
	}
	// The manifest icon must be attached to rows and the management list.
	if !strings.HasSuffix(results[0].IconPath, "icon.svg") {
		t.Fatalf("search row IconPath = %q, want icon.svg", results[0].IconPath)
	}
	for _, info := range m.ListPlugins() {
		if info.ID == "com.kyvro.github" && !strings.HasSuffix(info.IconPath, "icon.svg") {
			t.Fatalf("PluginInfo.IconPath = %q, want icon.svg", info.IconPath)
		}
	}

	// owner/repo adds a direct repo row ahead of the search row.
	results = m.Provider().Search(context.Background(), "gh wailsapp/wails")
	if len(results) != 2 {
		t.Fatalf("want repo+search rows, got %+v", results)
	}
	if results[0].PrimaryAction.Kind != core.ActionOpenURL ||
		results[0].PrimaryAction.Arg != "https://github.com/wailsapp/wails" {
		t.Fatalf("repo row action = %+v", results[0].PrimaryAction)
	}

	// Ordinary gh-prefixed words must fall through to the apps provider.
	if got := m.Provider().Search(context.Background(), "ghost"); len(got) != 0 {
		t.Fatalf("plain gh-words must not match, got %+v", got)
	}
	if got := m.Provider().Search(context.Background(), "gh  "); len(got) != 0 {
		t.Fatalf("blank input must not match, got %+v", got)
	}
}
