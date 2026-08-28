// Package web implements the fallback provider: when nothing else matches,
// offer to search the web for the raw query. Results open the default
// browser via a https URL, which the platform layer handles generically.
package web

import (
	"context"
	"fmt"
	"net/url"

	"kyvro/internal/core"
)

// Provider offers a single "Search Google for '<query>'" fallback row.
type Provider struct{}

// New creates a web fallback provider.
func New() *Provider { return &Provider{} }

// ID implements core.Provider.
func (p *Provider) ID() string { return "web" }

// Search implements core.Provider. Empty queries yield nothing; the
// provider is a pure tail fallback and relies on the engine appending it
// after higher-priority providers.
func (p *Provider) Search(_ context.Context, query string) []core.SearchResult {
	if query == "" {
		return nil
	}
	return []core.SearchResult{{
		ID:       "web:" + query,
		Kind:     core.KindURL,
		Title:    fmt.Sprintf("Search Google for %q", query),
		Subtitle: "open in default browser",
		PrimaryAction: core.Action{
			Kind: core.ActionOpenURL,
			Arg:  "https://www.google.com/search?q=" + url.QueryEscape(query),
		},
		Score: 0, // always below any real fuzzy match
	}}
}
