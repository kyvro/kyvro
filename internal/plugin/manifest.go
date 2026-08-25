package plugin

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	// HostVersion is the plugin platform version advertised to manifests.
	HostVersion = "0.1.0"
	// SchemaVersion is the only manifest schema this host understands.
	SchemaVersion = 1
	// ManifestFile is the manifest file name inside a plugin version dir.
	ManifestFile = "plugin.json"
	// CurrentFile optionally pins the active version directory (spec §11).
	CurrentFile = "current.json"
)

// reverseDomainRe matches reverse-domain plugin ids such as
// "com.example.encode": at least two lowercase labels.
var reverseDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Author is the manifest author block.
type Author struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Command is a statically declared command (spec §4).
type Command struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

// Manifest is the parsed plugin.json.
type Manifest struct {
	SchemaVersion    int       `json:"schemaVersion"`
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Version          string    `json:"version"`
	Description      string    `json:"description,omitempty"`
	Author           *Author   `json:"author,omitempty"`
	Main             string    `json:"main"`
	Icon             string    `json:"icon,omitempty"`
	MinHostVersion   string    `json:"minHostVersion"`
	Platforms        []string  `json:"platforms,omitempty"`
	ActivationEvents []string  `json:"activationEvents,omitempty"`
	Permissions      []string  `json:"permissions,omitempty"`
	Commands         []Command `json:"commands,omitempty"`

	// SearchPrefixes and CommandEvents are derived from ActivationEvents
	// at parse time for fast lookup by the provider.
	SearchPrefixes  []string
	CommandEventIDs map[string]bool
}

// ParseManifest decodes and validates manifest bytes. Validation failures
// return INVALID_ARGUMENT, except host/platform incompatibilities which
// return INCOMPATIBLE_VERSION.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, Errorf("", ErrInvalidArgument, "manifest is not valid JSON: %v", err)
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, Errorf(m.ID, ErrIncompatibleVersion,
			"schemaVersion %d unsupported (host supports %d)", m.SchemaVersion, SchemaVersion)
	}
	if !reverseDomainRe.MatchString(m.ID) {
		return nil, Errorf(m.ID, ErrInvalidArgument, "id %q is not a reverse-domain identifier", m.ID)
	}
	if !validSemver(m.Version) {
		return nil, Errorf(m.ID, ErrInvalidArgument, "version %q is not valid SemVer", m.Version)
	}
	if !validSemver(m.MinHostVersion) {
		return nil, Errorf(m.ID, ErrInvalidArgument, "minHostVersion %q is not valid SemVer", m.MinHostVersion)
	}
	if semver.Compare("v"+m.MinHostVersion, "v"+HostVersion) > 0 {
		return nil, Errorf(m.ID, ErrIncompatibleVersion,
			"minHostVersion %s exceeds host %s", m.MinHostVersion, HostVersion)
	}
	if err := validateRelPath(m.ID, "main", m.Main); err != nil {
		return nil, err
	}
	if m.Icon != "" {
		if err := validateRelPath(m.ID, "icon", m.Icon); err != nil {
			return nil, err
		}
	}
	if len(m.Platforms) > 0 && !contains(m.Platforms, runtime.GOOS) {
		return nil, Errorf(m.ID, ErrIncompatibleVersion,
			"platform %s not supported by plugin (wants %v)", runtime.GOOS, m.Platforms)
	}

	commandIDs := make(map[string]bool, len(m.Commands))
	for i, c := range m.Commands {
		if c.ID == "" {
			return nil, Errorf(m.ID, ErrInvalidArgument, "commands[%d].id is empty", i)
		}
		if commandIDs[c.ID] {
			return nil, Errorf(m.ID, ErrInvalidArgument, "duplicate command id %q", c.ID)
		}
		commandIDs[c.ID] = true
		if c.Title == "" {
			m.Commands[i].Title = c.ID
		}
	}
	if m.Name == "" {
		m.Name = m.ID
	}

	prefixes, err := parseActivationEvents(&m, commandIDs)
	if err != nil {
		return nil, err
	}
	m.SearchPrefixes = prefixes
	m.CommandEventIDs = commandIDs
	return &m, nil
}

// parseActivationEvents validates activationEvents and returns the
// onSearchPrefix prefixes. onCommand events must reference declared commands.
func parseActivationEvents(m *Manifest, commandIDs map[string]bool) ([]string, error) {
	var prefixes []string
	for i, ev := range m.ActivationEvents {
		switch {
		case strings.HasPrefix(ev, "onCommand:"):
			id := strings.TrimPrefix(ev, "onCommand:")
			if !commandIDs[id] {
				return nil, Errorf(m.ID, ErrInvalidArgument,
					"activationEvents[%d] %q references an undeclared command", i, ev)
			}
		case strings.HasPrefix(ev, "onSearchPrefix:"):
			pfx := strings.TrimPrefix(ev, "onSearchPrefix:")
			if pfx == "" {
				return nil, Errorf(m.ID, ErrInvalidArgument,
					"activationEvents[%d] %q has an empty prefix", i, ev)
			}
			prefixes = append(prefixes, strings.ToLower(pfx))
		default:
			return nil, Errorf(m.ID, ErrInvalidArgument,
				"activationEvents[%d] %q is not a known activation event", i, ev)
		}
	}
	return prefixes, nil
}

// validateRelPath enforces a relative, non-escaping path for a manifest
// file reference (main, icon).
func validateRelPath(pluginID, field, value string) error {
	if value == "" {
		return Errorf(pluginID, ErrInvalidArgument, "%s is empty", field)
	}
	cleaned := path.Clean(value)
	if path.IsAbs(cleaned) || cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") {
		return Errorf(pluginID, ErrInvalidArgument, "%s %q must be a relative path inside the plugin directory", field, value)
	}
	return nil
}

func validSemver(v string) bool {
	return v != "" && semver.IsValid("v"+v)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// currentPin is the content of current.json (spec §11).
type currentPin struct {
	Version string `json:"version"`
}

// ResolveVersionDir picks the code directory for a plugin installed at
// pluginDir (= <plugins root>/<id>). If current.json pins a valid, installed
// version directory, it wins; otherwise the highest SemVer child directory
// containing a plugin.json is used. Non-semver or manifest-less directories
// are ignored.
func ResolveVersionDir(pluginDir string) (string, error) {
	if data, err := os.ReadFile(filepath.Join(pluginDir, CurrentFile)); err == nil {
		var pin currentPin
		if json.Unmarshal(data, &pin) == nil && validSemver(pin.Version) {
			dir := filepath.Join(pluginDir, pin.Version)
			if manifestExists(dir) {
				return dir, nil
			}
		}
	}

	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return "", Errorf(filepath.Base(pluginDir), ErrInvalidArgument, "read plugin dir: %v", err)
	}
	best := ""
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !validSemver(name) {
			continue // junk directories are ignored
		}
		dir := filepath.Join(pluginDir, name)
		if !manifestExists(dir) {
			continue
		}
		if best == "" || semver.Compare("v"+name, "v"+filepath.Base(best)) > 0 {
			best = dir
		}
	}
	if best == "" {
		return "", Errorf(filepath.Base(pluginDir), ErrInvalidArgument,
			"no version directory with a %s under %s", ManifestFile, pluginDir)
	}
	return best, nil
}

func manifestExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ManifestFile))
	return err == nil && !info.IsDir()
}

// DisplayName returns a human-friendly plugin label.
func (m *Manifest) DisplayName() string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// LoadManifestFile reads and parses the manifest at dir/plugin.json.
func LoadManifestFile(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, Errorf(filepath.Base(dir), ErrInvalidArgument, "read %s: %v", ManifestFile, err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if dirName := filepath.Base(filepath.Dir(dir)); m.ID != dirName {
		return nil, Errorf(m.ID, ErrInvalidArgument,
			"manifest id %q does not match install directory %q", m.ID, dirName)
	}
	return m, nil
}
