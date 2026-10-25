package plugin

import "strings"

// unimplemented lists permission capabilities the host knows how to parse
// but does not implement in this version. Calling into any of them yields
// CAPABILITY_UNAVAILABLE.
var unimplemented = map[string]bool{
	"filesystem": true,
	"shell":      true,
	"clipboard":  true,
	"secrets":    true,
	"system":     true,
}

// GrantDecision decides which of a plugin's requested permissions are
// granted. It returns the subset of requested names that should be granted.
// A nil GrantDecision grants declared storage, background and scoped network. Reserved for the
// settings UI so per-plugin grants can be wired in without refactoring.
type GrantDecision func(pluginID string, requested []string) []string

// defaultGrant grants the implemented, manifest-declared capabilities.
func defaultGrant(_ string, requested []string) []string {
	var out []string
	for _, name := range requested {
		if permissionBase(name) == "storage" || name == "background" || strings.HasPrefix(name, "network:") {
			out = append(out, name)
		}
	}
	return out
}

// permissionBase returns the capability part of a permission name, i.e.
// "storage" for both "storage" and "storage:scoped" (spec §9).
func permissionBase(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i]
	}
	return name
}

// PermissionSet is the resolved permission state of one loaded plugin:
// default-deny, with explicit manifest grants. It is safe for concurrent use.
type PermissionSet struct {
	granted map[string]bool // exact names granted to the plugin
}

// ParsePermissions resolves the manifest requests against the grant decision.
// Unknown capabilities are treated as denied at Require time.
func ParsePermissions(pluginID string, requested []string, grant GrantDecision) *PermissionSet {
	if grant == nil {
		grant = defaultGrant
	}
	allowed := make(map[string]bool, len(requested))
	for _, name := range grant(pluginID, requested) {
		if contains(requested, name) {
			allowed[name] = true
		}
	}
	return &PermissionSet{granted: allowed}
}

// Granted reports whether name (or its bare capability) was granted.
func (p *PermissionSet) Granted(name string) bool {
	if p.granted[name] {
		return true
	}
	if i := strings.IndexByte(name, ':'); i > 0 {
		return p.granted[name[:i]]
	}
	return false
}

// Require enforces a permission:
//   - implemented + granted          → nil
//   - known but unimplemented in V1  → CAPABILITY_UNAVAILABLE
//   - anything else                  → PERMISSION_DENIED
func (p *PermissionSet) Require(pluginID, name string) error {
	base := permissionBase(name)
	if unimplemented[base] {
		return Errorf(pluginID, ErrCapabilityUnavailable,
			"permission %q requested a capability this host version does not provide", name)
	}
	if p.Granted(name) {
		return nil
	}
	return Errorf(pluginID, ErrPermissionDenied, "permission %q not granted", name)
}
