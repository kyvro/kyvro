package folders

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kyvro/internal/core"
)

var scanTime = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func mkdirAll(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(entries []core.FolderIndexEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strings.TrimPrefix(e.Path, "/"))
	}
	return out
}

func mustContain(t *testing.T, entries []core.FolderIndexEntry, path string) {
	t.Helper()
	for _, e := range entries {
		if e.Path == path {
			return
		}
	}
	t.Fatalf("%s missing from %+v", path, names(entries))
}

func TestScanDepthOneIndexesDirectChildrenOnly(t *testing.T) {
	root := mkdirAll(t, "alpha", "beta/inner", "beta/inner/deeper")
	entries, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: root, MaxDepth: 1}, scanTime)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	mustContain(t, entries, filepath.Join(root, "alpha"))
	mustContain(t, entries, filepath.Join(root, "beta"))
	for _, e := range entries {
		if strings.Contains(e.Path, "inner") {
			t.Fatalf("depth-1 scan indexed a nested dir: %+v", names(entries))
		}
	}
}

func TestScanDepthTwo(t *testing.T) {
	root := mkdirAll(t, "a/b/c")
	entries, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: root, MaxDepth: 2}, scanTime)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	mustContain(t, entries, filepath.Join(root, "a"))
	mustContain(t, entries, filepath.Join(root, "a", "b"))
	for _, e := range entries {
		if strings.HasSuffix(e.Path, filepath.Join("a", "b", "c")) {
			t.Fatalf("depth-2 scan indexed level 3: %+v", names(entries))
		}
	}
}

func TestScanSkipsExcludedAndHidden(t *testing.T) {
	root := mkdirAll(t,
		"proj/.git/hooks",
		"proj/node_modules/pkg",
		"proj/vendor/x",
		"proj/dist",
		"proj/build/out",
		"proj/.next/cache",
		"proj/.turbo",
		"proj/.cache/tmp",
		"proj/.hidden/child",
		"proj/real",
	)
	entries, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: filepath.Join(root, "proj"), MaxDepth: 4}, scanTime)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	mustContain(t, entries, filepath.Join(root, "proj", "real"))
	for _, e := range entries {
		base := filepath.Base(e.Path)
		if _, bad := excludedNames[base]; bad || (len(base) > 1 && base[0] == '.') {
			t.Fatalf("excluded/hidden dir indexed: %s", e.Path)
		}
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	root := mkdirAll(t, "real", "link-target")
	if err := os.Symlink(filepath.Join(root, "link-target"), filepath.Join(root, "real", "link")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	entries, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: filepath.Join(root, "real"), MaxDepth: 3}, scanTime)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, e := range entries {
		if filepath.Base(e.Path) == "link" {
			t.Fatal("symlinked directory was indexed")
		}
	}
}

func TestScanCancel(t *testing.T) {
	root := mkdirAll(t, "a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, core.FolderSource{ID: "s", Path: root, MaxDepth: 1}, scanTime); err == nil {
		t.Fatal("cancelled scan must fail")
	}
}

func TestScanRootValidation(t *testing.T) {
	// Missing root.
	if _, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: "/nonexistent/definitely", MaxDepth: 1}, scanTime); err == nil {
		t.Fatal("missing root must fail")
	}
	// File root.
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: f, MaxDepth: 1}, scanTime); err == nil {
		t.Fatal("file root must fail")
	}
	// Invalid depth.
	if _, err := Scan(context.Background(), core.FolderSource{ID: "s", Path: t.TempDir(), MaxDepth: 0}, scanTime); err == nil {
		t.Fatal("depth 0 must fail")
	}
}

func TestScanEntryShape(t *testing.T) {
	root := mkdirAll(t, "kyvro")
	entries, err := Scan(context.Background(), core.FolderSource{ID: "sid", Path: root, MaxDepth: 1}, scanTime)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Scan = %+v, %v", entries, err)
	}
	e := entries[0]
	wantPath := filepath.Join(root, "kyvro")
	if e.ID != "folder:"+wantPath || e.Name != "kyvro" || e.Path != wantPath || e.SourceID != "sid" {
		t.Fatalf("entry = %+v", e)
	}
	if len(e.SearchKeys) != 1 || e.SearchKeys[0] != "kyvro" {
		t.Fatalf("search keys = %+v", e.SearchKeys)
	}
	if !e.UpdatedAt.Equal(scanTime) {
		t.Fatalf("UpdatedAt = %v", e.UpdatedAt)
	}
}
