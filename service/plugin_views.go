package service

import (
	"context"
	"fmt"
	"kyvro/internal/core"
	"kyvro/internal/plugin"
)

// OpenPluginView only opens an open-view action from the current result session.
func (s *SearchService) OpenPluginView(id, actionID string) (*plugin.ViewPage, error) {
	s.mu.Lock()
	result, ok := s.last[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no search result with id %q", id)
	}
	action := result.PrimaryAction
	if actionID != "" {
		item, ok := findActionItem(result.Actions, actionID)
		if !ok {
			return nil, fmt.Errorf("unknown result action")
		}
		action = item.Action
	}
	if action.Kind != core.ActionPluginView || s.mgr == nil {
		return nil, fmt.Errorf("plugin view unavailable")
	}
	page, err := s.mgr.OpenView(context.Background(), action)
	if err != nil {
		return nil, err
	}
	s.recordUsage(id)
	return page, nil
}

func (s *SearchService) PluginViewEvent(sessionID, event string) (*plugin.ViewPage, error) {
	if s.mgr == nil {
		return nil, fmt.Errorf("plugin system unavailable")
	}
	return s.mgr.ViewEvent(context.Background(), sessionID, event)
}

func (s *SearchService) ClosePluginView(sessionID string) {
	if s.mgr != nil {
		s.mgr.CloseView(sessionID)
	}
}
