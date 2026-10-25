package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/dop251/goja"
	"kyvro/internal/core"
)

// ViewNode is a declarative component, never HTML, CSS or executable code.
type ViewNode struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	Title    string      `json:"title,omitempty"`
	Tone     string      `json:"tone,omitempty"`
	Gap      string      `json:"gap,omitempty"`
	Value    float64     `json:"value"`
	Max      float64     `json:"max"`
	Event    string      `json:"event,omitempty"`
	Children []*ViewNode `json:"children,omitempty"`
}

type ViewDefinition struct {
	Title     string            `json:"title"`
	Root      *ViewNode         `json:"root"`
	Shortcuts map[string]string `json:"shortcuts,omitempty"`
}

// ViewPage carries an opaque session ID; events can only target the active page.
type ViewPage struct {
	SessionID  string
	Definition *ViewDefinition
}

type viewSession struct {
	token      string
	pluginID   string
	viewID     string
	args       []string
	rt         *jsRuntime
	definition *ViewDefinition
}

func convertView(pluginID string, value any) (*ViewDefinition, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 256<<10 {
		return nil, Errorf(pluginID, ErrInvalidArgument, "view must be JSON with at most 256KiB")
	}
	var view ViewDefinition
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, Errorf(pluginID, ErrInvalidArgument, "invalid view: %v", err)
	}
	count := 0
	var validate func(*ViewNode, int) bool
	validate = func(node *ViewNode, depth int) bool {
		count++
		if node == nil || depth > 16 || count > 256 || len(node.Text) > 16384 || len(node.Title) > 1024 {
			return false
		}
		switch node.Type {
		case "column", "row", "card":
			if node.Event != "" {
				return false
			}
		case "text":
			if node.Event != "" || len(node.Children) > 0 {
				return false
			}
		case "progress":
			if node.Max <= 0 || node.Value < 0 || node.Value > node.Max || math.IsNaN(node.Value) || math.IsInf(node.Value, 0) || len(node.Children) > 0 || node.Event != "" {
				return false
			}
		case "button":
			if strings.TrimSpace(node.Text) == "" || strings.TrimSpace(node.Event) == "" || len(node.Event) > 128 || len(node.Children) > 0 {
				return false
			}
		default:
			return false
		}
		if node.Tone != "" && node.Tone != "default" && node.Tone != "muted" && node.Tone != "success" && node.Tone != "warning" && node.Tone != "danger" {
			return false
		}
		if node.Gap != "" && node.Gap != "small" && node.Gap != "medium" && node.Gap != "large" {
			return false
		}
		for _, child := range node.Children {
			if !validate(child, depth+1) {
				return false
			}
		}
		return true
	}
	if len(view.Title) > 1024 || !validate(view.Root, 0) {
		return nil, Errorf(pluginID, ErrInvalidArgument, "invalid view component tree")
	}
	for key, event := range view.Shortcuts {
		if (key != "left" && key != "right" && key != "enter") || strings.TrimSpace(event) == "" || len(event) > 128 {
			return nil, Errorf(pluginID, ErrInvalidArgument, "invalid view shortcut")
		}
	}
	return &view, nil
}

func (r *jsRuntime) renderView(ctx context.Context, viewID string, args []string, event string) (*ViewDefinition, error) {
	if r.renderViewFn == nil {
		return nil, Errorf(r.pluginID, ErrInvalidArgument, "module does not export renderView")
	}
	value, err := r.call(ctx, ActionTimeout, func(vm *goja.Runtime, deadline time.Time) (any, error) {
		if event != "" {
			if r.viewEventFn == nil {
				return nil, Errorf(r.pluginID, ErrInvalidArgument, "module does not export onViewEvent")
			}
			result, err := r.viewEventFn(goja.Undefined(), vm.ToValue(viewID), vm.ToValue(event), vm.ToValue(append([]string{}, args...)))
			if err != nil {
				return nil, wrapJS(r.pluginID, err)
			}
			if _, err = r.await(vm, result, deadline); err != nil {
				return nil, err
			}
		}
		result, err := r.renderViewFn(goja.Undefined(), vm.ToValue(viewID), vm.ToValue(append([]string{}, args...)))
		if err != nil {
			return nil, wrapJS(r.pluginID, err)
		}
		return r.await(vm, result, deadline)
	})
	if err != nil {
		return nil, err
	}
	return convertView(r.pluginID, value)
}

func (m *Manager) viewRuntime(pluginID string) (*jsRuntime, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lp := m.plugins[pluginID]
	if lp == nil || lp.disabled {
		return nil, Errorf(pluginID, ErrPluginException, "plugin unavailable or disabled")
	}
	return lp.rt, nil
}

// OpenView is reached through an authorized result action, not a search-index update.
func (m *Manager) OpenView(ctx context.Context, action core.Action) (*ViewPage, error) {
	if action.Kind != core.ActionPluginView {
		return nil, Errorf(action.PluginID, ErrInvalidArgument, "action is not open-view")
	}
	m.viewMu.Lock()
	defer m.viewMu.Unlock()
	rt, err := m.viewRuntime(action.PluginID)
	if err != nil {
		return nil, err
	}
	definition, err := rt.renderView(ctx, action.ActionID, action.Args, "")
	if err != nil {
		return nil, err
	}
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	m.activeView = &viewSession{token: token, pluginID: action.PluginID, viewID: action.ActionID, args: append([]string{}, action.Args...), rt: rt, definition: definition}
	return &ViewPage{SessionID: token, Definition: definition}, nil
}

func viewHasEvent(view *ViewDefinition, event string) bool {
	for _, value := range view.Shortcuts {
		if value == event {
			return true
		}
	}
	var containsEvent func(*ViewNode) bool
	containsEvent = func(node *ViewNode) bool {
		if node.Type == "button" && node.Event == event {
			return true
		}
		for _, child := range node.Children {
			if containsEvent(child) {
				return true
			}
		}
		return false
	}
	return containsEvent(view.Root)
}

func (m *Manager) ViewEvent(ctx context.Context, token, event string) (*ViewPage, error) {
	m.viewMu.Lock()
	defer m.viewMu.Unlock()
	session := m.activeView
	if session == nil || token != session.token || !viewHasEvent(session.definition, event) {
		return nil, Errorf("", ErrInvalidArgument, "unknown view session or event")
	}
	rt, err := m.viewRuntime(session.pluginID)
	if err != nil {
		return nil, err
	}
	if rt != session.rt {
		m.activeView = nil
		return nil, Errorf(session.pluginID, ErrInvalidArgument, "view expired after plugin reload")
	}
	definition, err := rt.renderView(ctx, session.viewID, session.args, event)
	if err != nil {
		return nil, err
	}
	session.definition = definition
	return &ViewPage{SessionID: session.token, Definition: definition}, nil
}

func (m *Manager) CloseView(token string) {
	m.viewMu.Lock()
	defer m.viewMu.Unlock()
	if m.activeView != nil && m.activeView.token == token {
		m.activeView = nil
	}
}
