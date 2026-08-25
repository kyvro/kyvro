// Package plugin implements the Kyvro plugin platform (spec M0+M1):
// manifest loading, permission checks, a goja-based JS runtime per plugin,
// plugin-scoped storage and integration with the search engine via a
// aggregated Provider. The package is pure Go: it must not import Wails,
// and goja must not leak outside it.
package plugin

import (
	"errors"
	"fmt"
)

// ErrorCode enumerates plugin platform error codes (spec §18). NETWORK_BLOCKED
// and HOST_API_ERROR are defined for spec fidelity; no V1 host API can
// produce them yet.
type ErrorCode string

const (
	ErrPermissionDenied      ErrorCode = "PERMISSION_DENIED"
	ErrTimeout               ErrorCode = "TIMEOUT"
	ErrCapabilityUnavailable ErrorCode = "CAPABILITY_UNAVAILABLE"
	ErrInvalidArgument       ErrorCode = "INVALID_ARGUMENT"
	ErrNetworkBlocked        ErrorCode = "NETWORK_BLOCKED"
	ErrHostAPIError          ErrorCode = "HOST_API_ERROR"
	ErrPluginException       ErrorCode = "PLUGIN_EXCEPTION"
	ErrIncompatibleVersion   ErrorCode = "INCOMPATIBLE_VERSION"
)

// PluginError is the error type exchanged with plugins and surfaced to the
// host. Every load, search and action failure carries one.
type PluginError struct {
	PluginID string
	Code     ErrorCode
	Message  string
}

func (e *PluginError) Error() string {
	if e.PluginID == "" {
		return fmt.Sprintf("plugin error %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("plugin %s: %s: %s", e.PluginID, e.Code, e.Message)
}

// Errorf builds a PluginError.
func Errorf(pluginID string, code ErrorCode, format string, args ...any) *PluginError {
	return &PluginError{PluginID: pluginID, Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the ErrorCode carried by err (false when err is not a
// PluginError or is nil).
func CodeOf(err error) (ErrorCode, bool) {
	var pe *PluginError
	if errors.As(err, &pe) {
		return pe.Code, true
	}
	return "", false
}
