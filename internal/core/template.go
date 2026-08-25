// Package core provides extensible template rendering for plugins and snippets.
package core

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// TemplateFunc is a function that can be registered by plugins and used in templates.
// It receives the arguments (as strings) and returns the rendered result.
type TemplateFunc func(args ...string) (string, error)

// templateRegistry manages template functions registered by plugins.
type templateRegistry struct {
	mu    sync.RWMutex
	funcs map[string]TemplateFunc
}

// global registry for template functions
var globalRegistry = &templateRegistry{
	funcs: make(map[string]TemplateFunc),
}

// RegisterTemplateFunc registers a template function that can be used in templates.
// If a function with the same name already exists, it will be replaced.
func RegisterTemplateFunc(name string, fn TemplateFunc) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.funcs[name] = fn
}

// GetTemplateFunc returns a registered template function, or nil if not found.
func GetTemplateFunc(name string) TemplateFunc {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	return globalRegistry.funcs[name]
}

// ListTemplateFuncs returns the names of all registered template functions.
func ListTemplateFuncs() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	names := make([]string, 0, len(globalRegistry.funcs))
	for name := range globalRegistry.funcs {
		names = append(names, name)
	}
	return names
}

// UnregisterTemplateFunc removes a registered template function.
func UnregisterTemplateFunc(name string) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	delete(globalRegistry.funcs, name)
}

// templateRenderer handles dynamic template expressions.
type templateRenderer struct {
	mu    sync.RWMutex
	funcs map[string]TemplateFunc
}

// newTemplateRenderer creates a template renderer with plugin functions.
// If funcs is nil, it uses the global registry.
func newTemplateRenderer(funcs map[string]TemplateFunc) *templateRenderer {
	if funcs == nil {
		funcs = make(map[string]TemplateFunc)
		globalRegistry.mu.RLock()
		for name, fn := range globalRegistry.funcs {
			funcs[name] = fn
		}
		globalRegistry.mu.RUnlock()
	}
	return &templateRenderer{funcs: funcs}
}

// Render resolves dynamic template expressions in the input string.
// Template syntax: ${funcName("arg1","arg2")}
//
// Examples:
//   ${date("YYYY-MM-DD")} - current date (if date plugin is installed)
//   ${upper("hello")} - uppercase text (if text plugin is installed)
func (r *templateRenderer) Render(template string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return templateRE.ReplaceAllStringFunc(template, func(expr string) string {
		m := templateRE.FindStringSubmatch(expr)
		if len(m) != 3 {
			return expr
		}

		funcName := m[1]
		argsStr := m[2]

		// Parse arguments
		var args []string
		if argsStr != "" {
			args = parseArgs(argsStr)
		}

		// Call the registered function
		if fn, ok := r.funcs[funcName]; ok {
			result, err := fn(args...)
			if err != nil {
				return fmt.Sprintf("[ERROR: %s]", err)
			}
			return result
		}

		// Function not found, return original expression
		return expr
	})
}

// parseArgs parses arguments from the template string.
// Handles simple quoted strings like "format" or 'format'.
func parseArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	// Handle quoted strings
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		return []string{s[1 : len(s)-1]}
	}

	// Handle comma-separated values
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		// Trim quotes from each part
		parts[i] = strings.Trim(parts[i], "\"'")
	}
	return parts
}

// templateRE matches ${func("arg1","arg2")} expressions.
var templateRE = regexp.MustCompile(`\$\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\(([^)]*)\)\s*\}`)

// Default template renderer
var defaultRenderer = newTemplateRenderer(nil)

// RenderTemplate renders a template string with dynamic expressions.
// This is the main entry point for template rendering.
func RenderTemplate(template string) string {
	return defaultRenderer.Render(template)
}
