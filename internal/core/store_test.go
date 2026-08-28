package core

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFolderSourceCRUDRoundTrip(t *testing.T) {
	s := openTestStore(t)
	created := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	src := FolderSource{
		ID:        "abc123",
		Path:      "/Users/alice/Code",
		MaxDepth:  2,
		Enabled:   true,
		CreatedAt: created,
		UpdatedAt: created,
	}
	if err := s.PutFolderSource(src); err != nil {
		t.Fatalf("PutFolderSource: %v", err)
	}
	got, err := s.GetFolderSource("abc123")
	if err != nil {
		t.Fatalf("GetFolderSource: %v", err)
	}
	if got != src {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, src)
	}

	// Update overwrites the same key.
	src.MaxDepth = 1
	if err := s.PutFolderSource(src); err != nil {
		t.Fatalf("PutFolderSource(update): %v", err)
	}
	got, err = s.GetFolderSource("abc123")
	if err != nil || got.MaxDepth != 1 {
		t.Fatalf("update not applied: %+v (%v)", got, err)
	}

	if err := s.DeleteFolderSource("abc123"); err != nil {
		t.Fatalf("DeleteFolderSource: %v", err)
	}
	if _, err := s.GetFolderSource("abc123"); err == nil {
		t.Fatal("deleted source still retrievable")
	}
}

func TestFolderSourceDeleteAbsentIsNoop(t *testing.T) {
	s := openTestStore(t)
	if err := s.DeleteFolderSource("nope"); err != nil {
		t.Fatalf("delete of absent source must be a no-op, got %v", err)
	}
}

func TestListFolderSourcesOrdered(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	// Insertion order intentionally shuffled.
	srcs := []FolderSource{
		{ID: "zz", Path: "/c", MaxDepth: 1, Enabled: true, CreatedAt: base, UpdatedAt: base},
		{ID: "aa", Path: "/a", MaxDepth: 1, Enabled: true, CreatedAt: base.Add(time.Hour), UpdatedAt: base},
		{ID: "mm", Path: "/b", MaxDepth: 1, Enabled: false, CreatedAt: base, UpdatedAt: base},
	}
	for _, src := range srcs {
		if err := s.PutFolderSource(src); err != nil {
			t.Fatalf("PutFolderSource(%s): %v", src.ID, err)
		}
	}
	got, err := s.ListFolderSources()
	if err != nil {
		t.Fatalf("ListFolderSources: %v", err)
	}
	want := []string{"mm", "zz", "aa"} // CreatedAt asc, then ID asc
	if len(got) != len(want) {
		t.Fatalf("got %d sources, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestOpenStoreCreatesFolderSourcesBucket(t *testing.T) {
	s := openTestStore(t)
	// A read of the bucket must not panic or error on a fresh database.
	if err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketFolderSources) == nil {
			return errors.New("folder-sources bucket missing")
		}
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}
}
