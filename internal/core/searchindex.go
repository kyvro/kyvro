package core

import "github.com/sahilm/fuzzy"

// SearchIndex is a prebuilt in-memory fuzzy-search index over a slice of
// items. Keys are flattened once at construction time; the search path only
// reads them, never rebuilds key slices (spec §8).
//
// It is generic and therefore must never appear in a Wails-bound method
// signature.
type SearchIndex[T any] struct {
	// Items holds the indexed items in stable order.
	Items []T
	// Keys holds every search key, mapped back to items via ItemOf.
	Keys []string
	// ItemOf maps each key index to its item index.
	ItemOf []int
}

// BuildSearchIndex flattens items into a key set via keysOf (each item may
// contribute several keys; per-item duplicates are skipped) and returns the
// immutable index.
func BuildSearchIndex[T any](items []T, keysOf func(T) []string) *SearchIndex[T] {
	var keys []string
	var itemOf []int
	for i, item := range items {
		seen := make(map[string]struct{})
		for _, k := range keysOf(item) {
			if k == "" {
				continue
			}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
			itemOf = append(itemOf, i)
		}
	}
	return &SearchIndex[T]{Items: items, Keys: keys, ItemOf: itemOf}
}

// Find runs a fuzzy match over the prebuilt keys. Each item keeps its best
// score across all of its keys; hits are returned in score-descending order
// (ties keep item order). An empty query returns nil — callers decide what
// "surface defaults" means.
func (ix *SearchIndex[T]) Find(query string) []SearchHit[T] {
	if query == "" || ix.Len() == 0 {
		return nil
	}
	matches := fuzzy.Find(query, ix.Keys)
	best := make([]float64, len(ix.Items))
	for i := range best {
		best[i] = -1
	}
	for _, m := range matches {
		item := ix.ItemOf[m.Index]
		if s := float64(m.Score); s > best[item] {
			best[item] = s
		}
	}
	hits := make([]SearchHit[T], 0, len(ix.Items))
	for i, s := range best {
		if s >= 0 {
			hits = append(hits, SearchHit[T]{Item: ix.Items[i], Score: s})
		}
	}
	// Insertion sort by score descending: hit lists are small and usually
	// nearly sorted; stability preserves item order on ties.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].Score > hits[j-1].Score; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	return hits
}

// Len reports the number of indexed items.
func (ix *SearchIndex[T]) Len() int { return len(ix.Items) }

// SearchHit pairs an indexed item with its fuzzy score.
type SearchHit[T any] struct {
	Item  T
	Score float64
}
