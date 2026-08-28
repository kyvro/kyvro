package web

import (
	"context"
	"testing"
)

func TestFallbackEntry(t *testing.T) {
	p := New()

	if got := p.Search(context.Background(), ""); got != nil {
		t.Fatalf("empty query must yield nothing, got %+v", got)
	}

	got := p.Search(context.Background(), "go generics")
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	r := got[0]
	if r.ID != "web:go generics" {
		t.Fatalf("id = %q", r.ID)
	}
	if r.PrimaryAction.Kind != 1 { // ActionOpenURL
		t.Fatalf("kind = %d, want ActionOpenURL", r.PrimaryAction.Kind)
	}
	if r.PrimaryAction.Arg != "https://www.google.com/search?q=go+generics" {
		t.Fatalf("unexpected action arg: %q", r.PrimaryAction.Arg)
	}
}
