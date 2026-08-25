package core

import (
	"math"
	"time"
)

// Frecency scoring weights. The combined score is
//
//	score = fuzzyScore + freqWeight*log2(count+1) + recencyWeight*decay(age)
//
// so raw fuzzy relevance always dominates, while frequently and recently
// used items get a bounded boost that can reorder equally-good matches.
const (
	freqWeight    = 8.0
	recencyWeight = 12.0
	// recencyHalfLife controls how fast the recency boost fades; after
	// roughly this many hours the boost is halved.
	recencyHalfLife = 72 * time.Hour
)

// Frecency returns the usage-based score component for the given usage
// snapshot. It is deterministic for a fixed (usage, now) pair, which keeps
// ranking tests reproducible.
func Frecency(count int64, lastUsed time.Time, now time.Time) float64 {
	s := freqWeight * math.Log2(float64(count)+1)
	if !lastUsed.IsZero() {
		age := max(now.Sub(lastUsed), 0)
		s += recencyWeight * math.Exp(-float64(age)/float64(recencyHalfLife)*math.Ln2)
	}
	return s
}
