package plugin

import (
	"fmt"

	"kyvro/internal/core"
)

// scoreHintMax caps the score a plugin may hint (spec §7: ranking authority
// stays with the core engine).
const scoreHintMax = 50.0

// convertResults converts a value exported from the JS runtime (expected:
// []any of map[string]any result objects) into core results. Result IDs are
// namespaced as "plugin:<pluginID>:<id>"; iconPath (the manifest icon) is
// attached for the UI. Invalid entries are dropped and counted; a non-array
// value is an INVALID_ARGUMENT error.
func convertResults(pluginID, iconPath string, v any) ([]core.SearchResult, int, error) {
	if v == nil {
		return nil, 0, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, 0, Errorf(pluginID, ErrInvalidArgument,
			"expected an array of results, got %T", v)
	}
	results := make([]core.SearchResult, 0, len(items))
	dropped := 0
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			dropped++
			continue
		}
		r, ok := convertResult(pluginID, iconPath, obj)
		if !ok {
			dropped++
			continue
		}
		results = append(results, r)
	}
	return results, dropped, nil
}

// convertResult converts one result object. The first valid action becomes
// the primary Action executed on Enter.
func convertResult(pluginID, iconPath string, obj map[string]any) (core.SearchResult, bool) {
	id, _ := obj["id"].(string)
	title, _ := obj["title"].(string)
	if id == "" || title == "" {
		return core.SearchResult{}, false
	}
	rawActions, _ := obj["actions"].([]any)
	if len(rawActions) == 0 {
		return core.SearchResult{}, false
	}
	action, ok := convertAction(pluginID, rawActions[0])
	if !ok {
		return core.SearchResult{}, false
	}
	subtitle, _ := obj["subtitle"].(string)
	return core.SearchResult{
		ID:       "plugin:" + pluginID + ":" + id,
		Title:    title,
		Subtitle: subtitle,
		Action:   action,
		Score:    clampScore(obj["scoreHint"]),
		IconPath: iconPath,
	}, true
}

// convertAction maps a JS action object to a core Action.
func convertAction(pluginID string, v any) (core.Action, bool) {
	obj, ok := v.(map[string]any)
	if !ok {
		return core.Action{}, false
	}
	switch t, _ := obj["type"].(string); t {
	case "open-url":
		url, _ := obj["url"].(string)
		if url == "" {
			return core.Action{}, false
		}
		return core.Action{Kind: core.ActionOpenURL, Arg: url}, true
	case "copy":
		value, _ := obj["value"].(string)
		if value == "" {
			return core.Action{}, false
		}
		return core.Action{Kind: core.ActionCopyText, Arg: value}, true
	case "callback":
		id, _ := obj["id"].(string)
		if id == "" {
			return core.Action{}, false
		}
		return core.Action{
			Kind:     core.ActionPlugin,
			PluginID: pluginID,
			ActionID: id,
			Args:     toStringSlice(obj["args"]),
		}, true
	default:
		return core.Action{}, false
	}
}

func clampScore(v any) float64 {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int64:
		f = float64(n)
	case int:
		f = float64(n)
	default:
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > scoreHintMax {
		return scoreHintMax
	}
	return f
}

func toStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// commandResult builds the surfaced row for a manifest command. Args
// forwards the current query (V1 simplification; a real argument DSL is M2).
func commandResult(m *Manifest, cmd Command, query string, score float64, iconPath string) core.SearchResult {
	subtitle := cmd.Subtitle
	if subtitle == "" {
		subtitle = m.DisplayName()
	}
	return core.SearchResult{
		ID:       fmt.Sprintf("plugin:%s:cmd:%s", m.ID, cmd.ID),
		Title:    cmd.Title,
		Subtitle: subtitle,
		Action: core.Action{
			Kind:     core.ActionPlugin,
			PluginID: m.ID,
			ActionID: cmd.ID,
			Args:     []string{query},
		},
		Score:    score,
		IconPath: iconPath,
	}
}
