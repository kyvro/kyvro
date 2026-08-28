package folders

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kyvro/internal/core"
	"kyvro/internal/indexcache"
)

// newTestController returns a controller over fresh data dirs plus a close
// func (bbolt is exclusive-lock, so close before reopening the same file).
func newTestController(t *testing.T) (*Controller, string, func()) {
	t.Helper()
	dir := t.TempDir()
	open := func() (*Controller, func()) {
		store, err := core.OpenStore(filepath.Join(dir, "data.db"))
		if err != nil {
			t.Fatal(err)
		}
		cache, err := indexcache.Open(filepath.Join(dir, "cache"))
		if err != nil {
			t.Fatal(err)
		}
		return NewController(store, cache), func() { store.Close() }
	}
	c, closeStore := open()
	t.Cleanup(closeStore)
	return c, dir, func() { closeStore() }
}

func makeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"alpha", "beta/nested", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func titles(p *Provider) []string {
	var out []string
	for _, r := range p.Search(context.Background(), "") {
		out = append(out, r.Title)
	}
	return out
}

func hasTitle(p *Provider, title string) bool {
	for _, s := range titles(p) {
		if s == title {
			return true
		}
	}
	return false
}

func TestAddSourceScansAndWiresEverything(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)

	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if !src.Enabled || src.MaxDepth != 1 || !filepath.IsAbs(src.Path) {
		t.Fatalf("source = %+v", src)
	}

	// Provider serves the scanned dirs (hidden excluded, depth 1).
	if !hasTitle(c.Provider(), "alpha") || !hasTitle(c.Provider(), "beta") {
		t.Fatalf("provider = %v", titles(c.Provider()))
	}
	if hasTitle(c.Provider(), "nested") || hasTitle(c.Provider(), ".hidden") {
		t.Fatalf("depth/exclusion broken: %v", titles(c.Provider()))
	}

	// Cache file contains the entries.
	f, err := c.cache.LoadFolderIndex()
	if err != nil || len(f.Entries) != 2 {
		t.Fatalf("cache = %+v, %v", f, err)
	}

	// Sources reports the count.
	infos := c.Sources()
	if len(infos) != 1 || infos[0].IndexedCount != 2 {
		t.Fatalf("infos = %+v", infos)
	}
}

func TestAddSourceRejectsDuplicatePathAndBadDepth(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)

	if _, err := c.AddSource(context.Background(), root, 0); err == nil {
		t.Fatal("depth 0 must be rejected")
	}
	if _, err := c.AddSource(context.Background(), filepath.Join(root, "nope"), 1); err == nil {
		t.Fatal("missing root must be rejected")
	}
	if _, err := c.AddSource(context.Background(), root, 1); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := c.AddSource(context.Background(), root+string(filepath.Separator), 1); err == nil {
		t.Fatal("duplicate path (even with trailing slash) must be rejected")
	}
}

func TestRemoveSourceClearsAllThreeLayers(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)
	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.RemoveSource(src.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if got := titles(c.Provider()); got != nil {
		t.Fatalf("provider not cleared: %v", got)
	}
	f, _ := c.cache.LoadFolderIndex()
	if len(f.Entries) != 0 {
		t.Fatalf("cache not cleared: %+v", f.Entries)
	}
	if sources, _ := c.store.ListFolderSources(); len(sources) != 0 {
		t.Fatalf("store not cleared: %+v", sources)
	}
}

func TestDisableKeepsCacheAndReenableRestores(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)
	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.SetEnabled(context.Background(), src.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got := titles(c.Provider()); got != nil {
		t.Fatalf("disabled source still searchable: %v", got)
	}
	f, _ := c.cache.LoadFolderIndex()
	if len(f.Entries) != 2 {
		t.Fatalf("disable must keep cache entries: %+v", f.Entries)
	}

	// Re-enable: cached entries are searchable again immediately.
	if err := c.SetEnabled(context.Background(), src.ID, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !hasTitle(c.Provider(), "alpha") {
		t.Fatalf("re-enable must restore cache into provider: %v", titles(c.Provider()))
	}
}

func TestRefreshFailureKeepsOldEntriesAndRecordsError(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)
	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Destroy the root, then force a refresh.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := c.RefreshSource(context.Background(), src.ID); err == nil {
		t.Fatal("refresh of a vanished root must fail")
	}

	if !hasTitle(c.Provider(), "alpha") {
		t.Fatalf("failed refresh must keep provider entries: %v", titles(c.Provider()))
	}
	f, _ := c.cache.LoadFolderIndex()
	if len(f.Entries) != 2 {
		t.Fatalf("failed refresh must keep cache entries: %+v", f.Entries)
	}
	infos := c.Sources()
	if len(infos) != 1 || infos[0].LastScanError == "" {
		t.Fatalf("scan error not recorded: %+v", infos)
	}
}

func TestRefreshDisabledSourceIsExplicitError(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)
	src, _ := c.AddSource(context.Background(), root, 1)
	if err := c.SetEnabled(context.Background(), src.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := c.RefreshSource(context.Background(), src.ID); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("refresh(disabled) = %v, want explicit disabled error", err)
	}
}

func TestRefreshAllIsolatesFailures(t *testing.T) {
	c, _, _ := newTestController(t)
	live := makeTree(t)
	dead := makeTree(t)
	s1, err := c.AddSource(context.Background(), live, 1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := c.AddSource(context.Background(), dead, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dead); err != nil {
		t.Fatal(err)
	}

	err = c.RefreshAll(context.Background())
	if err == nil {
		t.Fatal("aggregate error expected when one source fails")
	}
	// The live source must still have been refreshed.
	infos := c.Sources()
	byID := map[string]core.FolderSourceInfo{}
	for _, info := range infos {
		byID[info.Source.ID] = info
	}
	if byID[s1.ID].LastScanError != "" || byID[s1.ID].IndexedCount != 2 {
		t.Fatalf("live source damaged by sibling failure: %+v", byID[s1.ID])
	}
	if byID[s2.ID].LastScanError == "" {
		t.Fatalf("dead source error not recorded: %+v", byID[s2.ID])
	}
}

func TestLoadAtStartupSeedsFromCacheWithoutScan(t *testing.T) {
	c, dir, closeFirst := newTestController(t)
	root := makeTree(t)
	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatal(err)
	}

	// reopen builds a controller over the same data dirs; the caller must
	// close the previous controller first (bbolt is single-writer).
	reopen := func(t *testing.T) (*Controller, func()) {
		t.Helper()
		store, err := core.OpenStore(filepath.Join(dir, "data.db"))
		if err != nil {
			t.Fatal(err)
		}
		cache, err := indexcache.Open(filepath.Join(dir, "cache"))
		if err != nil {
			t.Fatal(err)
		}
		return NewController(store, cache), func() { store.Close() }
	}

	closeFirst() // release the bbolt lock before reopening

	c2, close2 := reopen(t)
	if err := c2.LoadAtStartup(); err != nil {
		t.Fatalf("LoadAtStartup: %v", err)
	}
	if !hasTitle(c2.Provider(), "alpha") {
		t.Fatalf("startup seed missing entries: %v", titles(c2.Provider()))
	}

	// Disabled sources are filtered out at startup.
	close2()
	c4, close4 := reopen(t)
	if err := c4.SetEnabled(context.Background(), src.ID, false); err != nil {
		t.Fatalf("disable via reopened controller: %v", err)
	}
	close4()
	c3, close3 := reopen(t)
	defer close3()
	if err := c3.LoadAtStartup(); err != nil {
		t.Fatal(err)
	}
	if got := titles(c3.Provider()); got != nil {
		t.Fatalf("disabled source leaked into startup seed: %v", got)
	}
}

func TestAddSourceExpandsTilde(t *testing.T) {
	c, _, _ := newTestController(t)
	// Redirect HOME so the scan of "~" stays inside test-visible dirs.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	src, err := c.AddSource(context.Background(), "~", 1)
	if err != nil {
		t.Fatalf("AddSource(~): %v", err)
	}
	if src.Path != fakeHome {
		t.Fatalf("path = %q, want %q", src.Path, fakeHome)
	}
}

func TestSourceStatusScanningFlagLifecycle(t *testing.T) {
	c, _, _ := newTestController(t)
	root := makeTree(t)
	src, err := c.AddSource(context.Background(), root, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range c.Sources() {
		if info.Source.ID == src.ID && info.Scanning {
			t.Fatal("scanning flag stuck after sync add")
		}
	}
}
