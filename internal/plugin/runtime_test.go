package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kyvro/internal/core"
)

func TestRuntimeSearchConvertsResults(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "basic.js"))
	if !rt.HasProvider() {
		t.Fatal("provider not detected")
	}
	results, err := rt.Search(context.Background(), "b64 hello", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.ID != "plugin:com.example.test:first" {
		t.Errorf("ID = %q, want namespaced", r.ID)
	}
	if r.Title != "P:b64 hello" || r.Subtitle != "sub" {
		t.Errorf("title/subtitle = %q/%q", r.Title, r.Subtitle)
	}
	if r.Score != scoreHintMax {
		t.Errorf("scoreHint 60 must clamp to %v, got %v", scoreHintMax, r.Score)
	}
	if r.Action.Kind != core.ActionCopyText || r.Action.Arg != "copied" {
		t.Errorf("action = %+v", r.Action)
	}
}

func TestRuntimeSearchDropsInvalidEntries(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "bad_entries.js"))
	results, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 valid results, got %d: %+v", len(results), results)
	}
	ids := results[0].ID + "," + results[1].ID
	if !strings.Contains(ids, ":ok") || !strings.Contains(ids, ":score") {
		t.Errorf("unexpected survivors: %s", ids)
	}
	for _, r := range results {
		if r.Action.Kind == core.ActionPlugin && r.Action.PluginID == "" {
			t.Errorf("callback action lost its plugin id: %+v", r.Action)
		}
	}
}

func TestRuntimeSearchInfiniteLoopTimesOut(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "spin.js"))
	_, err := rt.Search(context.Background(), "b64 x", 60*time.Millisecond)
	if code, ok := CodeOf(err); !ok || code != ErrTimeout {
		t.Fatalf("want TIMEOUT, got %v", err)
	}
	if rt.Strikes() != 1 {
		t.Errorf("strikes = %d, want 1", rt.Strikes())
	}
}

func TestRuntimeTimeoutDropsLateResultAndRecovers(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "spin_then_return.js"))
	_, err := rt.Search(context.Background(), "b64 x", 80*time.Millisecond)
	if code, ok := CodeOf(err); !ok || code != ErrTimeout {
		t.Fatalf("want TIMEOUT, got %v", err)
	}
	// The worker finishes the slow call (~300ms) and its late result must be
	// dropped, not delivered to the abandoned request. A follow-up call with
	// a generous budget must succeed and reset the strike counter.
	results, err := rt.Search(context.Background(), "b64 again", time.Second)
	if err != nil {
		t.Fatalf("runtime did not recover: %v", err)
	}
	if len(results) != 1 || results[0].Title != "late" {
		t.Fatalf("recovered search returned %+v", results)
	}
	if rt.Strikes() != 0 {
		t.Errorf("strikes = %d after success, want 0", rt.Strikes())
	}
}

func TestRuntimeSearchException(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "throw.js"))
	_, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
	code, ok := CodeOf(err)
	if !ok || code != ErrPluginException {
		t.Fatalf("want PLUGIN_EXCEPTION, got %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("exception message lost: %v", err)
	}
}

func TestRuntimeStorageAbsentWithoutPermission(t *testing.T) {
	m := manifestMap(t, validManifest)
	delete(m, "permissions")
	// Loading fails loudly if ctx.storage leaks without a grant.
	rt, err := buildRuntime(t, marshal(t, m), fixtureJS(t, "no_storage.js"), nil)
	if err != nil {
		t.Fatalf("no-storage plugin must load: %v", err)
	}
	rt.Shutdown()
}

func TestRuntimeStorageRoundtripAndPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	run := func() string {
		store, err := core.OpenStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		rt, err := buildRuntime(t, []byte(validManifest), fixtureJS(t, "storage.js"), store)
		if err != nil {
			store.Close()
			t.Fatalf("load: %v", err)
		}
		results, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
		rt.Shutdown()
		store.Close()
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("want 1 result, got %+v", results)
		}
		return results[0].Title
	}
	// First activate stores runs=1; reloading the plugin over the same db
	// (version switch or restart) must see the persisted value.
	if got := run(); got != "runs:1" {
		t.Fatalf("first run = %q, want runs:1", got)
	}
	if got := run(); got != "runs:2" {
		t.Fatalf("second run = %q, want runs:2 (storage must persist)", got)
	}
}

func TestRuntimeRunActionCallback(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "basic.js"))
	results, err := rt.RunAction(context.Background(), "test.cmd", []string{"arg1"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "action:test.cmd:arg1" {
		t.Fatalf("unexpected results: %+v", results)
	}
	// First action is the callback; it must round-trip through the plugin.
	r := results[0]
	if r.Action.Kind != core.ActionPlugin || r.Action.ActionID != "back" {
		t.Fatalf("primary action = %+v", r.Action)
	}
	second, err := rt.RunAction(context.Background(), r.Action.ActionID, r.Action.Args, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Title != "action:back:x" {
		t.Fatalf("callback round-trip failed: %+v", second)
	}
}

func TestRuntimeActivatePromiseAwaited(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "async.js"))
	results, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "ready:yes" {
		t.Fatalf("activate promise was not awaited: %+v", results)
	}
}

func TestRuntimeIconAttachedWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(fixtureJS(t, "basic.js")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	withIcon := manifestMap(t, validManifest)
	withIcon["icon"] = "icon.svg"
	m, err := ParseManifest(marshal(t, withIcon))
	if err != nil {
		t.Fatal(err)
	}

	rt, err := newRuntime(m, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Shutdown)
	results, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].IconPath != filepath.Join(dir, "icon.svg") {
		t.Fatalf("IconPath = %q, want %s", results[0].IconPath, filepath.Join(dir, "icon.svg"))
	}

	// A declared but missing icon file degrades to no icon (no load error).
	noFile := manifestMap(t, validManifest)
	noFile["icon"] = "missing.svg"
	m2, err := ParseManifest(marshal(t, noFile))
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "index.js"), []byte(fixtureJS(t, "basic.js")), 0o644); err != nil {
		t.Fatal(err)
	}
	rt2, err := newRuntime(m2, dir2, nil)
	if err != nil {
		t.Fatalf("missing icon must not fail the load: %v", err)
	}
	t.Cleanup(rt2.Shutdown)
	results, err = rt2.Search(context.Background(), "b64 x", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].IconPath != "" {
		t.Fatalf("missing icon must yield empty IconPath, got %+v", results)
	}
}

func TestRuntimeWithoutProvider(t *testing.T) {
	rt := newTestRuntime(t, validManifest, fixtureJS(t, "minimal.js"))
	if rt.HasProvider() {
		t.Fatal("minimal.js must not expose a provider")
	}
	results, err := rt.Search(context.Background(), "b64 x", 150*time.Millisecond)
	if err != nil || results != nil {
		t.Fatalf("search without provider = %+v, %v", results, err)
	}
}
