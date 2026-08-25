package plugin

import "testing"

func TestParsePermissionsDefaultPolicy(t *testing.T) {
	p := ParsePermissions("com.example.test",
		[]string{"storage", "network:api.github.com", "clipboard:read", "shell:git"}, nil)

	if !p.Granted("storage") {
		t.Error("storage should be granted under the default policy")
	}
	for _, name := range []string{"network:api.github.com", "clipboard:read", "shell:git"} {
		if p.Granted(name) {
			t.Errorf("%s should not be granted by default", name)
		}
	}
}

func TestParsePermissionsCustomGrant(t *testing.T) {
	grant := func(pluginID string, requested []string) []string {
		if pluginID == "com.example.test" {
			return []string{"network:api.github.com"}
		}
		return nil
	}
	p := ParsePermissions("com.example.test", []string{"storage", "network:api.github.com"}, grant)
	if p.Granted("storage") {
		t.Error("custom decision should override the default storage grant")
	}
	if !p.Granted("network:api.github.com") {
		t.Error("network should be granted by the custom decision")
	}
}

func TestRequire(t *testing.T) {
	granted := ParsePermissions("p", []string{"storage"}, nil)
	denied := ParsePermissions("p", nil, nil)

	if err := granted.Require("p", "storage"); err != nil {
		t.Errorf("granted storage must pass: %v", err)
	}
	if code, ok := CodeOf(denied.Require("p", "storage")); !ok || code != ErrPermissionDenied {
		t.Errorf("undeclared storage must be PERMISSION_DENIED, got %v", denied.Require("p", "storage"))
	}
	for _, name := range []string{"network:api.github.com", "filesystem:~/**", "clipboard:read", "secrets:x", "background", "system:open-url"} {
		err := granted.Require("p", name)
		code, ok := CodeOf(err)
		if !ok || code != ErrCapabilityUnavailable {
			t.Errorf("%s must be CAPABILITY_UNAVAILABLE in V1, got %v", name, err)
		}
	}
	if code, ok := CodeOf(granted.Require("p", "unknown-capability")); !ok || code != ErrPermissionDenied {
		t.Errorf("unknown capability must be PERMISSION_DENIED, got %v", granted.Require("p", "unknown-capability"))
	}
}
