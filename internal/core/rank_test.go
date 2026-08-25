package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestEngine(t *testing.T, providers ...Provider) (*Engine, *Store) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewEngine(providers, store, 0), store
}

// fixedNow makes scoring deterministic.
var fixedNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)

// staticProvider always returns the same result set, regardless of query.
type staticProvider struct {
	id      string
	entries []SearchResult
}

func (p *staticProvider) ID() string { return p.id }

func (p *staticProvider) Search(_ context.Context, _ string) []SearchResult {
	return p.entries
}

func TestFrecencyComponents(t *testing.T) {
	zero := Frecency(0, time.Time{}, fixedNow)
	if zero != 0 {
		t.Fatalf("unused item should score 0, got %v", zero)
	}

	once := Frecency(1, fixedNow, fixedNow)
	if once <= 0 {
		t.Fatalf("recent single use should boost, got %v", once)
	}

	// More uses, same recency => higher score.
	twice := Frecency(2, fixedNow, fixedNow)
	if twice <= once {
		t.Fatalf("count=2 (%v) should outrank count=1 (%v)", twice, once)
	}

	// Same count, older => lower score.
	old := Frecency(1, fixedNow.Add(-14*24*time.Hour), fixedNow)
	if old >= once {
		t.Fatalf("old use (%v) should rank below recent (%v)", old, once)
	}
}

func TestStoreRoundtrip(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Record("com.apple.Safari", fixedNow); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.Record("com.apple.Safari", fixedNow.Add(time.Hour)); err != nil {
		t.Fatalf("record: %v", err)
	}
	u, err := store.Get("com.apple.Safari")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if u.Count != 2 {
		t.Fatalf("count = %d, want 2", u.Count)
	}
	if want := fixedNow.Add(time.Hour); !u.LastUsed.Equal(want) {
		t.Fatalf("lastUsed = %v, want %v", u.LastUsed, want)
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 || all["com.apple.Safari"].Count != 2 {
		t.Fatalf("unexpected snapshot: %+v", all)
	}
}

func TestEngineEmptyQueryOrdersByFrecency(t *testing.T) {
	eng, store := newTestEngine(t, &staticProvider{id: "apps", entries: []SearchResult{
		{ID: "a", Title: "Apple Notes"},
		{ID: "b", Title: "Safari"},
		{ID: "c", Title: "Terminal"},
	}})
	eng.SetNow(func() time.Time { return fixedNow })

	// Use Safari twice and Terminal once, Safari more recently.
	for i := 0; i < 2; i++ {
		if err := store.Record("b", fixedNow.Add(-time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Record("c", fixedNow.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := eng.Search(context.Background(), "")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	if got[0].ID != "b" || got[1].ID != "c" || got[2].ID != "a" {
		t.Fatalf("order = %s,%s,%s; want b,c,a", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestEngineFrecencyPromotionIsReproducible(t *testing.T) {
	apps := &staticProvider{id: "apps", entries: []SearchResult{
		{ID: "notes", Title: "Notes"},
		{ID: "safari", Title: "Safari"},
	}}
	eng, store := newTestEngine(t, apps)
	eng.SetNow(func() time.Time { return fixedNow })

	// Before any use: alphabetical tie-break.
	got := eng.Search(context.Background(), "")
	if got[0].ID != "notes" {
		t.Fatalf("cold order should be alphabetical, got %s first", got[0].ID)
	}

	// Use Safari, then search again: frecency must promote it to rank 1.
	if err := store.Record("safari", fixedNow); err != nil {
		t.Fatal(err)
	}
	got = eng.Search(context.Background(), "")
	if got[0].ID != "safari" {
		t.Fatalf("after launch, safari should be first, got %s", got[0].ID)
	}

	// And again with a fresh engine over the same store (persistence).
	eng2 := NewEngine([]Provider{apps}, store, 0)
	eng2.SetNow(func() time.Time { return fixedNow })
	got = eng2.Search(context.Background(), "")
	if got[0].ID != "safari" {
		t.Fatalf("after reload, safari should still be first, got %s", got[0].ID)
	}
}

func TestEngineFallbackStaysAtTail(t *testing.T) {
	eng, _ := newTestEngine(t,
		&staticProvider{id: "apps", entries: []SearchResult{
			{ID: "safari", Title: "Safari", Score: 50},
			{ID: "mail", Title: "Mail", Score: 40},
		}},
		&staticProvider{id: "web", entries: []SearchResult{
			{ID: "web:xyz", Title: "Search Google for 'xyz'", Score: 0},
		}},
	)
	eng.SetNow(func() time.Time { return fixedNow })

	got := eng.Search(context.Background(), "xyz")
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	if got[2].ID != "web:xyz" {
		t.Fatalf("web fallback must be last, got order %v,%v,%v", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestEngineTruncates(t *testing.T) {
	entries := make([]SearchResult, 20)
	for i := range entries {
		entries[i] = SearchResult{ID: string(rune('a' + i)), Title: string(rune('A' + i))}
	}
	eng, _ := newTestEngine(t, &staticProvider{id: "apps", entries: entries})
	got := eng.Search(context.Background(), "")
	if len(got) != DefaultLimit {
		t.Fatalf("got %d results, want %d", len(got), DefaultLimit)
	}
}
