package core

import (
	"context"
	"testing"
	"time"
)

// The general engine helpers (newTestEngine, staticProvider, fixedNow) live
// in rank_test.go; this file covers the search-path semantics added with
// folder search: ID dedup and provider-order guarantees.

func TestEngineDedupFirstProviderWins(t *testing.T) {
	eng, _ := newTestEngine(t,
		&staticProvider{id: "a", entries: []SearchResult{
			{ID: "dup", Title: "dup"}, {ID: "only-a", Title: "only-a"},
		}},
		&staticProvider{id: "b", entries: []SearchResult{
			{ID: "dup", Title: "dup"}, {ID: "only-b", Title: "only-b"},
		}},
	)
	got := eng.Search(context.Background(), "q")
	if len(got) != 3 || got[0].ID != "dup" || got[1].ID != "only-a" || got[2].ID != "only-b" {
		t.Fatalf("ids = %+v, want dup,only-a,only-b (first provider wins)", got)
	}
}

func TestEngineDedupWithinProvider(t *testing.T) {
	eng, _ := newTestEngine(t, &staticProvider{id: "a", entries: []SearchResult{
		{ID: "x", Title: "x", Score: 5},
		{ID: "x", Title: "x", Score: 1},
		{ID: "y", Title: "y", Score: 1},
	}})
	got := eng.Search(context.Background(), "q")
	if len(got) != 2 || got[0].ID != "x" || got[1].ID != "y" {
		t.Fatalf("intra-provider dup not folded: %+v", got)
	}
}

func TestEngineLimitBoundAfterDedup(t *testing.T) {
	eng, _ := newTestEngine(t,
		&staticProvider{id: "a", entries: []SearchResult{
			{ID: "d1", Title: "d1"}, {ID: "d2", Title: "d2"}, {ID: "d3", Title: "d3"},
		}},
		&staticProvider{id: "b", entries: []SearchResult{
			{ID: "d1", Title: "d1"}, {ID: "d2", Title: "d2"},
			{ID: "d3", Title: "d3"}, {ID: "d4", Title: "d4"}, {ID: "d5", Title: "d5"},
		}},
	)
	got := eng.Search(context.Background(), "q")
	if len(got) != 5 {
		t.Fatalf("got %d results, want 5 (d1..d3 shared, d4+d5 unique): %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, r := range got {
		if seen[r.ID] {
			t.Fatalf("duplicate %s in output", r.ID)
		}
		seen[r.ID] = true
	}
}

// The provider order [calc, apps, folders, plugins, web] must hold even
// when later providers hand the engine higher raw scores.
func TestEngineProviderOrderKeepsFoldersBeforePluginsAndWebLast(t *testing.T) {
	eng, _ := newTestEngine(t,
		&staticProvider{id: "calc", entries: []SearchResult{{ID: "calc:1", Title: "c1", Score: 1}}},
		&staticProvider{id: "apps", entries: []SearchResult{{ID: "app:x", Title: "ax", Score: 50}}},
		&staticProvider{id: "folders", entries: []SearchResult{{ID: "folder:y", Title: "fy", Score: 40}}},
		&staticProvider{id: "plugins", entries: []SearchResult{{ID: "plugin:z", Title: "pz", Score: 30}}},
		&staticProvider{id: "web", entries: []SearchResult{{ID: "web:q", Title: "wq", Score: 100}}},
	)
	got := eng.Search(context.Background(), "q")
	if len(got) != 5 {
		t.Fatalf("got %+v", got)
	}
	wantOrder := []string{"calc:1", "app:x", "folder:y", "plugin:z", "web:q"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("position %d = %s, want %s (all: %+v)", i, got[i].ID, id, got)
		}
	}
}

func TestEngineEmptyQueryWithEmptyProviders(t *testing.T) {
	eng, _ := newTestEngine(t, &staticProvider{id: "calc", entries: nil})
	if got := eng.Search(context.Background(), ""); got != nil {
		t.Fatalf("empty query = %+v", got)
	}
}

func TestEngineUsageKeyedByID(t *testing.T) {
	eng, store := newTestEngine(t, &staticProvider{id: "apps", entries: []SearchResult{
		{ID: "app:cold", Title: "cold"},
		{ID: "app:hot", Title: "hot"},
	}})
	eng.SetNow(func() time.Time { return fixedNow })
	if err := store.Record("app:hot", fixedNow); err != nil {
		t.Fatal(err)
	}
	got := eng.Search(context.Background(), "q")
	if len(got) != 2 || got[0].ID != "app:hot" {
		t.Fatalf("frecency must lift the used app: %+v", got)
	}
}
