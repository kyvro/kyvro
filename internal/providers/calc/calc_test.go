package calc

import (
	"context"
	"testing"

	"kyvro/internal/core"
)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		query string
		want  string // empty means: must not evaluate
	}{
		// arithmetic and precedence
		{"1+2*3", "7"},
		{"(1+2)*3", "9"},
		{"10-2-3", "5"},
		{"100/4/5", "5"},
		{"2^10", "1024"},
		{"2^3^2", "512"}, // right-associative
		{"2^-2", "0.25"},
		{"7%3", "1"},
		{"10/4", "2.5"},
		{"-5+3", "-2"},
		{"2*-3", "-6"},
		// normalisation: spaces, separators, CJK symbols
		{" 2 * (3+4) ", "14"},
		{"1,000+2", "1002"},
		{"3×3", "9"},
		{"10÷4", "2.5"},
		{"1 + 1", "2"},
		// float noise is cleaned up
		{"0.1+0.2", "0.3"},
		{"0.3*3", "0.9"},
		{"1/3*3", "1"},
		// rejections
		{"", ""},
		{"   ", ""},
		{"123", ""}, // bare number: no binary operator
		{"-5", ""},  // leading sign only: not an expression
		{"abc", ""},
		{"safa", ""},
		{"1+", ""},    // syntax error
		{"(1+2", ""},  // missing )
		{"1)", ""},    // trailing garbage
		{"1.2.3", ""}, // malformed number
		{"1/0", ""},   // division by zero
		{"5%0", ""},   // modulo by zero
		{"9^9^9", ""}, // overflow to +Inf
	}
	for _, tc := range cases {
		value, _, ok := Evaluate(tc.query)
		if tc.want == "" {
			if ok {
				t.Errorf("Evaluate(%q) = %q, want rejection", tc.query, value)
			}
			continue
		}
		if !ok {
			t.Errorf("Evaluate(%q) rejected, want %q", tc.query, tc.want)
			continue
		}
		if value != tc.want {
			t.Errorf("Evaluate(%q) = %q, want %q", tc.query, value, tc.want)
		}
	}
}

func TestSearchReturnsCopyableResult(t *testing.T) {
	p := New()
	if got := p.ID(); got != "calc" {
		t.Fatalf("ID = %q, want calc", got)
	}

	results := p.Search(context.Background(), "(1+2)*4")
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	r := results[0]
	if r.ID != "calc:(1+2)*4" || r.Title != "= 12" {
		t.Fatalf("bad result: %+v", r)
	}
	if r.PrimaryAction.Kind != core.ActionCopyText || r.PrimaryAction.Arg != "12" {
		t.Fatalf("bad action: %+v", r.PrimaryAction)
	}
	if r.Kind != core.KindText {
		t.Fatalf("kind = %q, want text", r.Kind)
	}

	// Non-expressions yield nothing.
	for _, q := range []string{"", "Safari", "gc", "42"} {
		if got := p.Search(context.Background(), q); got != nil {
			t.Errorf("Search(%q) = %+v, want nil", q, got)
		}
	}
}
