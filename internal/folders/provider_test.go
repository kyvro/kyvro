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

func entry(source, name, path string) core.FolderIndexEntry {
	return core.FolderIndexEntry{
		ID:         "folder:" + path,
		Name:       name,
		Path:       path,
		SourceID:   source,
		SearchKeys: []string{name},
		UpdatedAt:  time.Now(),
	}
}

func fixture() []core.FolderIndexEntry {
	return []core.FolderIndexEntry{
		entry("s1", "kyvro", "/tmp/roots/code/kyvro"),
		entry("s1", "launcher", "/tmp/roots/code/launcher"),
		entry("s2", "photos", "/tmp/roots/media/photos"),
	}
}

func TestEmptyQueryReturnsAllWithScoreZero(t *testing.T) {
	p := NewProvider(fixture())
	got := p.Search(context.Background(), "")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	for _, r := range got {
		if r.Score != 0 {
			t.Fatalf("empty-query score = %v, want 0", r.Score)
		}
	}
}

func TestMatchesBasenameOnly(t *testing.T) {
	p := NewProvider(fixture())
	// "roots" and "tmp" appear only in the path, never the basename.
	for _, q := range []string{"roots", "tmp", "code"} {
		if got := p.Search(context.Background(), q); got != nil {
			t.Fatalf("query %q matched path segments: %+v", q, got)
		}
	}
	got := p.Search(context.Background(), "kyv")
	if len(got) != 1 || got[0].Title != "kyvro" {
		t.Fatalf("kyv = %+v", got)
	}
}

func TestResultShapeMatchesSpec(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home dir unavailable: %v", err)
	}
	dir := filepath.Join(home, "Code", "kyvro")
	p := NewProvider([]core.FolderIndexEntry{entry("s1", "kyvro", dir)})
	got := p.Search(context.Background(), "kyv")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	r := got[0]
	if r.ID != "folder:"+dir || r.Kind != core.KindFolder || r.Title != "kyvro" {
		t.Fatalf("identity = %+v", r)
	}
	if want := "~" + strings.TrimPrefix(dir, home); r.Subtitle != want {
		t.Fatalf("subtitle = %q, want %q", r.Subtitle, want)
	}
	if got, _ := r.Data["path"].(string); got != dir {
		t.Fatalf("Data[path] = %v", r.Data["path"])
	}
	if r.PrimaryAction.Kind != core.ActionOpenPath || r.PrimaryAction.Arg != dir {
		t.Fatalf("primary = %+v", r.PrimaryAction)
	}
	if len(r.Actions) != 2 {
		t.Fatalf("actions = %+v", r.Actions)
	}
	reveal, copyPath := r.Actions[0], r.Actions[1]
	if reveal.ID != "reveal" || reveal.Shortcut != "cmd+enter" || reveal.Action.Kind != core.ActionRevealPath ||
		reveal.Action.Arg != dir {
		t.Fatalf("reveal action = %+v", reveal)
	}
	if copyPath.ID != "copy-path" || copyPath.Shortcut != "cmd+c" || copyPath.Action.Kind != core.ActionCopyText ||
		copyPath.Action.Arg != dir {
		t.Fatalf("copy action = %+v", copyPath)
	}
}

func TestSourceMutationsReflectedInSearch(t *testing.T) {
	p := NewProvider(fixture())

	p.DeleteSourceEntries("s1")
	if got := p.Search(context.Background(), ""); len(got) != 1 || got[0].Title != "photos" {
		t.Fatalf("after delete s1 = %+v", got)
	}

	p.ReplaceSourceEntries("s2", []core.FolderIndexEntry{entry("s2", "movies", "/tmp/roots/media/movies")})
	got := p.Search(context.Background(), "")
	if len(got) != 1 || got[0].Title != "movies" {
		t.Fatalf("after replace s2 = %+v", got)
	}

	p.ReplaceSourceEntries("s3", []core.FolderIndexEntry{entry("s3", "docs", "/tmp/roots/docs")})
	if got := p.Search(context.Background(), ""); len(got) != 2 {
		t.Fatalf("after add s3 = %+v", got)
	}

	counts := p.CountBySource()
	if counts["s2"] != 1 || counts["s3"] != 1 || counts["s1"] != 0 {
		t.Fatalf("CountBySource = %v", counts)
	}
	if e := p.EntriesForSource("s3"); len(e) != 1 || e[0].Name != "docs" {
		t.Fatalf("EntriesForSource(s3) = %+v", e)
	}
}

func TestEmptyProviderYieldsNothing(t *testing.T) {
	p := NewProvider(nil)
	if got := p.Search(context.Background(), "x"); got != nil {
		t.Fatalf("got %+v", got)
	}
	if got := p.Search(context.Background(), ""); got != nil {
		t.Fatalf("empty-query got %+v", got)
	}
}

func TestAbbrevHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	cases := map[string]string{
		home:                                 "~",
		filepath.Join(home, "Code"):          "~/Code",
		filepath.Join(home, "Code", "kyvro"): "~/Code/kyvro",
		"/Applications":                      "/Applications",
		// Prefix-match without a directory boundary must stay untouched.
		home + "x": home + "x",
	}
	for in, want := range cases {
		if got := AbbrevHome(in); got != want {
			t.Errorf("AbbrevHome(%q) = %q, want %q", in, got, want)
		}
	}
}
