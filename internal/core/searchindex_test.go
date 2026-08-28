package core

import (
	"strings"
	"testing"
)

type idxItem struct {
	Name string
	Alts []string
}

func buildIdx(items []idxItem) *SearchIndex[idxItem] {
	return BuildSearchIndex(items, func(it idxItem) []string {
		return append([]string{it.Name}, it.Alts...)
	})
}

func TestBuildSearchIndexFlattensKeys(t *testing.T) {
	ix := buildIdx([]idxItem{
		{Name: "Safari", Alts: []string{"Safari", "", "浏览器"}},
		{Name: "Notes"},
	})
	if ix.Len() != 2 {
		t.Fatalf("Len = %d, want 2", ix.Len())
	}
	// Per-item duplicates and empty keys are skipped: Safari→[Safari, 浏览器].
	if len(ix.Keys) != 3 {
		t.Fatalf("Keys = %+v, want 3 entries", ix.Keys)
	}
	if ix.ItemOf[0] != 0 || ix.ItemOf[1] != 0 || ix.ItemOf[2] != 1 {
		t.Fatalf("ItemOf = %v", ix.ItemOf)
	}
}

func TestFindTakesBestKeyPerItem(t *testing.T) {
	ix := buildIdx([]idxItem{
		{Name: "Safari", Alts: []string{"com.apple.Safari"}},
		{Name: "Google Chrome"},
	})
	hits := ix.Find("safa")
	if len(hits) != 1 || hits[0].Item.Name != "Safari" {
		t.Fatalf("safa should hit Safari only, got %+v", hits)
	}
	if hits[0].Score <= 0 {
		t.Fatalf("score = %v, want > 0", hits[0].Score)
	}
	if got := ix.Find("zzz"); len(got) != 0 {
		t.Fatalf("zzz matched %+v", got)
	}
	if got := ix.Find(""); got != nil {
		t.Fatalf("empty query must return nil, got %+v", got)
	}
}

func TestFindOrdersByScoreDescending(t *testing.T) {
	ix := buildIdx([]idxItem{
		{Name: "Notes"},
		{Name: "NotesApp"},
		{Name: "Unrelated"},
	})
	hits := ix.Find("notes")
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %+v", hits)
	}
	// "Notes" is an exact-length match and must outrank "NotesApp".
	if hits[0].Item.Name != "Notes" {
		t.Fatalf("top = %s, want Notes (all: %+v)", hits[0].Item.Name, hits)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("hits not score-descending: %+v", hits)
		}
	}
}

func TestFindStableOnTies(t *testing.T) {
	ix := buildIdx([]idxItem{
		{Name: "aa"},
		{Name: "ab"},
		{Name: "ac"},
	})
	hits := ix.Find("a")
	want := []string{"aa", "ab", "ac"}
	if len(hits) != len(want) {
		t.Fatalf("want %d hits, got %+v", len(want), hits)
	}
	for i, name := range want {
		if hits[i].Item.Name != name {
			t.Fatalf("tie order changed: want %v, got %+v", want, hits)
		}
		if !strings.HasPrefix(name, "a") {
			t.Fatal("unreachable")
		}
	}
}
