package core

import "context"

// Provider produces search results for a query. Implementations must be
// safe for concurrent use. The engine calls providers in priority order:
// results from earlier providers always rank above results from later ones,
// which is what keeps the web fallback pinned to the tail.
type Provider interface {
	// ID returns a stable provider identifier, e.g. "apps" or "web".
	ID() string
	// Search returns candidates for the query. An empty query means
	// "surface defaults" (e.g. the full app list ordered by frecency
	// later). Providers that have nothing to contribute return nil.
	Search(ctx context.Context, query string) []SearchResult
}
