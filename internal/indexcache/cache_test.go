package indexcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"kyvro/internal/core"
)

func appEntries(n int) []core.AppIndexEntry {
	out := make([]core.AppIndexEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, core.AppIndexEntry{
			ID:         "app:com.example." + string(rune('a'+i)),
			Name:       string(rune('A' + i)),
			Path:       "/Applications/" + string(rune('A'+i)) + ".app",
			SearchKeys: []string{string(rune('A' + i))},
		})
	}
	return out
}

func folderEntry(source, name string) core.FolderIndexEntry {
	return core.FolderIndexEntry{
		ID:         "folder:/root/" + name,
		Name:       name,
		Path:       "/root/" + name,
		SourceID:   source,
		SearchKeys: []string{name},
		UpdatedAt:  time.Now(),
	}
}

func TestOpenCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("cache dir not created: %v", err)
	}
	// Empty caches load as empty, not errors.
	f, err := c.LoadAppIndex()
	if err != nil || len(f.Entries) != 0 {
		t.Fatalf("LoadAppIndex on empty = %+v, %v", f, err)
	}
}

func TestAppIndexRoundTrip(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := appEntries(3)
	if err := c.SaveAppIndex(want); err != nil {
		t.Fatalf("SaveAppIndex: %v", err)
	}
	f, err := c.LoadAppIndex()
	if err != nil {
		t.Fatalf("LoadAppIndex: %v", err)
	}
	if len(f.Entries) != 3 || f.Entries[0].ID != want[0].ID || f.Version != core.IndexVersion {
		t.Fatalf("round trip mismatch: %+v", f)
	}
}

func TestFolderIndexPerSourceReplacement(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.ReplaceFolderEntriesForSource("s1", []core.FolderIndexEntry{folderEntry("s1", "a"), folderEntry("s1", "b")}); err != nil {
		t.Fatalf("replace s1: %v", err)
	}
	if err := c.ReplaceFolderEntriesForSource("s2", []core.FolderIndexEntry{folderEntry("s2", "c")}); err != nil {
		t.Fatalf("replace s2: %v", err)
	}

	// Mirror queries see both sources.
	if got := c.FolderEntriesForSource("s1"); len(got) != 2 {
		t.Fatalf("s1 mirror = %+v", got)
	}

	// Replacing s1 must not disturb s2, on disk and in memory.
	if err := c.ReplaceFolderEntriesForSource("s1", []core.FolderIndexEntry{folderEntry("s1", "z")}); err != nil {
		t.Fatalf("re-replace s1: %v", err)
	}
	f, err := c.LoadFolderIndex()
	if err != nil {
		t.Fatalf("LoadFolderIndex: %v", err)
	}
	names := map[string]int{}
	for _, e := range f.Entries {
		names[e.Name]++
	}
	if names["z"] != 1 || names["c"] != 1 || names["a"] != 0 || names["b"] != 0 {
		t.Fatalf("entries after replacement = %+v", f.Entries)
	}
	if got := c.FolderEntriesForSource("s2"); len(got) != 1 || got[0].Name != "c" {
		t.Fatalf("s2 mirror = %+v", got)
	}

	// Deletion only touches the deleted source.
	if err := c.DeleteFolderEntriesForSource("s1"); err != nil {
		t.Fatalf("delete s1: %v", err)
	}
	f, _ = c.LoadFolderIndex()
	if len(f.Entries) != 1 || f.Entries[0].Name != "c" {
		t.Fatalf("entries after delete = %+v", f.Entries)
	}
	if got := c.FolderEntriesForSource("s1"); got != nil {
		t.Fatalf("s1 mirror after delete = %+v", got)
	}
}

func TestFolderMirrorPreloadedByOpen(t *testing.T) {
	dir := t.TempDir()
	c1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c1.ReplaceFolderEntriesForSource("s1", []core.FolderIndexEntry{folderEntry("s1", "a")}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	// A second Open over the same directory must preload the mirror.
	c2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := c2.FolderEntriesForSource("s1"); len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("mirror not preloaded: %+v", got)
	}
}

func TestCorruptFilesDegradeToEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, appFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, folderFileName), []byte("[[[["), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, err := c.LoadAppIndex(); err != nil || len(f.Entries) != 0 {
		t.Fatalf("corrupt app index = %+v, %v (want empty, nil)", f, err)
	}
	if f, err := c.LoadFolderIndex(); err != nil || len(f.Entries) != 0 {
		t.Fatalf("corrupt folder index = %+v, %v (want empty, nil)", f, err)
	}
}

func TestFutureVersionDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, appFileName), []byte(`{"Version":99,"Entries":[{"ID":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if f, err := c.LoadAppIndex(); err != nil || len(f.Entries) != 0 {
		t.Fatalf("future version = %+v, %v (want empty, nil)", f, err)
	}
}

func TestWritesLeaveNoTmpFiles(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.SaveAppIndex(appEntries(1)); err != nil {
		t.Fatalf("SaveAppIndex: %v", err)
	}
	if err := c.ReplaceFolderEntriesForSource("s", []core.FolderIndexEntry{folderEntry("s", "a")}); err != nil {
		t.Fatalf("ReplaceFolderEntriesForSource: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Ext(f.Name()) == tmpSuffix {
			t.Fatalf("tmp leftover: %s", f.Name())
		}
	}
}
