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

// Command is a statically declared command (plugins.md §2.3). Its prefix
// joins the host command index; while the query starts with it the plugin's
// onAction is live-called with the command id as the actionId.
type Command struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Prefix string `json:"prefix"`
}

// Manifest is the parsed plugin.json.
type Manifest struct {
	SchemaVersion  int       `json:"schemaVersion"`
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	Description    string    `json:"description,omitempty"`
	Author         *Author   `json:"author,omitempty"`
	Main           string    `json:"main"`
	Icon           string    `json:"icon,omitempty"`
	MinHostVersion string    `json:"minHostVersion"`
	Platforms      []string  `json:"platforms,omitempty"`
	Permissions    []string  `json:"permissions,omitempty"`
	Commands       []Command `json:"commands,omitempty"`
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
	prefixes := make(map[string]bool, len(m.Commands))
	for i, c := range m.Commands {
		if c.ID == "" {
			return nil, Errorf(m.ID, ErrInvalidArgument, "commands[%d].id is empty", i)
		}
		if commandIDs[c.ID] {
			return nil, Errorf(m.ID, ErrInvalidArgument, "duplicate command id %q", c.ID)
		}
		commandIDs[c.ID] = true
		if strings.TrimSpace(c.Prefix) == "" {
			return nil, Errorf(m.ID, ErrInvalidArgument,
				"commands[%d] (%s) prefix is required and must not be empty", i, c.ID)
		}
		m.Commands[i].Prefix = strings.ToLower(strings.TrimSpace(c.Prefix))
		if prefixes[m.Commands[i].Prefix] {
			return nil, Errorf(m.ID, ErrInvalidArgument,
				"duplicate command prefix %q", m.Commands[i].Prefix)
		}
		prefixes[m.Commands[i].Prefix] = true
		if c.Title == "" {
			m.Commands[i].Title = c.ID
		}
	}
	if m.Name == "" {
		m.Name = m.ID
	}

	return &m, nil
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

// Install source markers persisted in current.json (docs/plugin-marketplace.md
// 「安装来源与自动升级」): market installs are auto-upgraded, local installs
// (offline import / manual copy) are user-managed and never auto-touched.
const (
	SourceMarket = "market"
	SourceLocal  = "local"
)

// currentPin is the content of current.json (spec §11). An empty Source is
// read as SourceLocal (manual directory copies carry no pin at all).
type currentPin struct {
	Version string `json:"version"`
	Source  string `json:"source,omitempty"`
}

// ReadCurrentPin reads the pin of an installed plugin (dir = <root>/<id>).
// ok is false when the file is absent or unparseable.
func ReadCurrentPin(pluginDir string) (currentPin, bool) {
	data, err := os.ReadFile(filepath.Join(pluginDir, CurrentFile))
	if err != nil {
		return currentPin{}, false
	}
	var pin currentPin
	if json.Unmarshal(data, &pin) != nil {
		return currentPin{}, false
	}
	return pin, true
}

// VersionNewer reports whether SemVer a is strictly newer than b.
func VersionNewer(a, b string) bool {
	return semver.Compare("v"+a, "v"+b) > 0
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
