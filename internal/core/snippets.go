// Snippet support for global text expansion.
package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Snippet represents a text expansion trigger and its replacement.
type Snippet struct {
	// Trigger is the text that triggers expansion (e.g., "dd").
	Trigger string `json:"trigger"`
	// Replacement is the text that replaces the trigger (e.g., "20260825").
	Replacement string `json:"replacement"`
	// CreatedAt is when this snippet was created.
	CreatedAt time.Time `json:"createdAt,omitempty"`
	// Enabled determines if this snippet is active.
	Enabled bool `json:"enabled"`
}

// Validate checks if the snippet is valid.
func (s *Snippet) Validate() error {
	if s.Trigger == "" {
		return fmt.Errorf("trigger cannot be empty")
	}
	if s.Replacement == "" {
		return fmt.Errorf("replacement cannot be empty")
	}
	if strings.ContainsAny(s.Trigger, "\n\r\t") {
		return fmt.Errorf("trigger cannot contain newline or tab characters")
	}
	if len(s.Trigger) > 100 {
		return fmt.Errorf("trigger too long (max 100 characters)")
	}
	if len(s.Replacement) > 10000 {
		return fmt.Errorf("replacement too long (max 10000 characters)")
	}
	return nil
}

// SnippetsService manages text snippets.
type SnippetsService struct {
	store *Store
	now   func() time.Time
}

// NewSnippetsService creates a snippets service.
func NewSnippetsService(store *Store) *SnippetsService {
	return &SnippetsService{store: store, now: time.Now}
}

const (
	snippetsNamespace = "snippets"
	snippetListKey    = "all"
)

// Add adds a new snippet. If a snippet with the same trigger exists,
// it will be replaced.
func (s *SnippetsService) Add(snippet Snippet) error {
	if err := snippet.Validate(); err != nil {
		return err
	}
	snippet.CreatedAt = s.now()
	snippet.Enabled = true

	list, err := s.list()
	if err != nil {
		return err
	}

	// Replace existing snippet with same trigger or append
	found := false
	for i, existing := range list {
		if existing.Trigger == snippet.Trigger {
			list[i] = snippet
			found = true
			break
		}
	}
	if !found {
		list = append(list, snippet)
	}

	return s.saveList(list)
}

// RenderSnippetReplacement resolves dynamic template expressions in a snippet
// replacement. Static replacement text is returned unchanged.
func RenderSnippetReplacement(replacement string, now time.Time) string {
	renderer := newTemplateRenderer(nil)
	return renderer.Render(replacement)
}

// Remove removes the snippet with the given trigger.
func (s *SnippetsService) Remove(trigger string) error {
	list, err := s.list()
	if err != nil {
		return err
	}

	filtered := make([]Snippet, 0, len(list))
	for _, sn := range list {
		if sn.Trigger != trigger {
			filtered = append(filtered, sn)
		}
	}

	if len(filtered) == len(list) {
		return fmt.Errorf("snippet %q not found", trigger)
	}

	return s.saveList(filtered)
}

// List returns all snippets.
func (s *SnippetsService) List() ([]Snippet, error) {
	return s.list()
}

// SetEnabled enables or disables a snippet.
func (s *SnippetsService) SetEnabled(trigger string, enabled bool) error {
	list, err := s.list()
	if err != nil {
		return err
	}

	found := false
	for i := range list {
		if list[i].Trigger == trigger {
			list[i].Enabled = enabled
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("snippet %q not found", trigger)
	}

	return s.saveList(list)
}

// GetEnabled returns only enabled snippets, keyed by trigger for fast lookup.
func (s *SnippetsService) GetEnabled() (map[string]Snippet, error) {
	list, err := s.list()
	if err != nil {
		return nil, err
	}

	result := make(map[string]Snippet)
	for _, sn := range list {
		if sn.Enabled {
			result[sn.Trigger] = sn
		}
	}
	return result, nil
}

func (s *SnippetsService) list() ([]Snippet, error) {
	if s.store == nil {
		return []Snippet{}, nil
	}

	v, ok, err := s.store.GetNS(snippetsNamespace, snippetListKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Snippet{}, nil
	}

	var list []Snippet
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		return nil, fmt.Errorf("unmarshal snippets: %w", err)
	}
	return list, nil
}

func (s *SnippetsService) saveList(list []Snippet) error {
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal snippets: %w", err)
	}
	return s.store.PutNS(snippetsNamespace, snippetListKey, string(data))
}
