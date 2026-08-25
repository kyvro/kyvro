// Package calc implements the in-search calculator: queries that look
// like arithmetic expressions evaluate to a copyable result, like
// Spotlight. The grammar is a small recursive-descent parser over
// float64 — no dependencies, no variables, no function calls.
package calc

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"kyvro/internal/core"
)

// Provider evaluates arithmetic-looking queries.
type Provider struct{}

// New creates a calculator provider.
func New() *Provider { return &Provider{} }

// ID implements core.Provider.
func (p *Provider) ID() string { return "calc" }

// Search implements core.Provider. It returns a single result when the
// query is a well-formed arithmetic expression with at least one binary
// operator, and nothing otherwise.
func (p *Provider) Search(_ context.Context, query string) []core.SearchResult {
	value, expr, ok := Evaluate(query)
	if !ok {
		return nil
	}
	return []core.SearchResult{{
		ID:       "calc:" + expr,
		Title:    "= " + value,
		Subtitle: expr + " · 回车复制结果",
		Action: core.Action{
			Kind: core.ActionCopyText,
			Arg:  value,
		},
	}}
}

// Evaluate normalises query and parses it as an arithmetic expression.
// ok is false when the query is not an expression (no digit, foreign
// character, no binary operator) or fails to evaluate (syntax error,
// division by zero, overflow to ±Inf).
func Evaluate(query string) (value, expr string, ok bool) {
	expr = normalise(query)
	if !looksArithmetic(expr) {
		return "", "", false
	}
	v, err := parseExpr(expr)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return "", "", false
	}
	return formatValue(v), expr, true
}

// normalise strips whitespace and thousand separators and maps the
// symbols produced by Chinese input methods to ASCII operators.
func normalise(q string) string {
	r := strings.NewReplacer(
		" ", "", "\t", "",
		",", "", "，", "",
		"×", "*", "÷", "/",
	)
	return r.Replace(strings.TrimSpace(q))
}

// looksArithmetic reports whether s consists solely of the calculator
// charset and contains a digit plus a binary operator (an operator
// somewhere past the first character, so a leading unary sign does not
// turn a bare number into an expression).
func looksArithmetic(s string) bool {
	hasDigit, hasOp := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '.' || c == '(' || c == ')':
			// structural, neither digit nor operator
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '%' || c == '^':
			if i > 0 {
				hasOp = true
			}
		default:
			return false
		}
	}
	return hasDigit && hasOp
}

// parseExpr parses a full expression and rejects trailing garbage.
func parseExpr(s string) (float64, error) {
	p := &parser{s: s}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.i != len(p.s) {
		return 0, fmt.Errorf("unexpected %q at %d", p.s[p.i], p.i)
	}
	return v, nil
}

// parser is a recursive-descent parser for:
//
//	expr   := term (('+'|'-') term)*
//	term   := factor (('*'|'/'|'%') factor)*
//	factor := unary ('^' factor)?          // right-associative power
//	unary  := ('+'|'-') unary | primary
//	primary:= number | '(' expr ')'
type parser struct {
	s string
	i int
}

var errDivideByZero = fmt.Errorf("division by zero")

func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		op := p.peek()
		if op != '+' && op != '-' {
			return v, nil
		}
		p.i++
		t, err := p.term()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += t
		} else {
			v -= t
		}
	}
}

func (p *parser) term() (float64, error) {
	v, err := p.factor()
	if err != nil {
		return 0, err
	}
	for {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			return v, nil
		}
		p.i++
		t, err := p.factor()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			v *= t
		case '/':
			if t == 0 {
				return 0, errDivideByZero
			}
			v /= t
		case '%':
			if t == 0 {
				return 0, errDivideByZero
			}
			v = math.Mod(v, t)
		}
	}
}

func (p *parser) factor() (float64, error) {
	v, err := p.unary()
	if err != nil {
		return 0, err
	}
	if p.peek() == '^' {
		p.i++
		t, err := p.factor() // recurse for right-associativity
		if err != nil {
			return 0, err
		}
		v = math.Pow(v, t)
	}
	return v, nil
}

func (p *parser) unary() (float64, error) {
	switch p.peek() {
	case '-':
		p.i++
		v, err := p.unary()
		return -v, err
	case '+':
		p.i++
		return p.unary()
	}
	return p.primary()
}

func (p *parser) primary() (float64, error) {
	if p.peek() == '(' {
		p.i++
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, fmt.Errorf("missing )")
		}
		p.i++
		return v, nil
	}
	return p.number()
}

func (p *parser) number() (float64, error) {
	start := p.i
	for p.i < len(p.s) && (isDigit(p.s[p.i]) || p.s[p.i] == '.') {
		p.i++
	}
	if start == p.i {
		return 0, fmt.Errorf("want number at %d", start)
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}

func (p *parser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// formatValue renders v without float64 noise. The shortest-round-trip
// form is re-parsed through 12 significant digits when it carries more
// digits than a user-typed expression warrants (0.1+0.2 → 0.3, not
// 0.30000000000000004).
func formatValue(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if countDigits(s) > 12 {
		if g, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 12, 64), 64); err == nil {
			s = strconv.FormatFloat(g, 'f', -1, 64)
		}
	}
	return s
}

func countDigits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if isDigit(s[i]) {
			n++
		}
	}
	return n
}
