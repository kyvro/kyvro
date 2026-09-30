package plugin

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Installer handles downloading and installing plugins from the registry.
type Installer struct {
	client      *http.Client
	pluginsRoot string
	registry    *RegistryClient
	installHook func(id, version string) // Callback for UI updates
}

// NewInstaller creates a plugin installer for the given plugins directory.
func NewInstaller(pluginsRoot string) *Installer {
	return &Installer{
		client:      &http.Client{Timeout: 30 * time.Second},
		pluginsRoot: pluginsRoot,
		registry:    NewRegistryClient(),
	}
}

// InstallFromGitHub installs a plugin from the official registry by ID. The
// registry entry already carries the version selected for this host
// (docs/plugin-marketplace.md「版本适配」).
func (in *Installer) InstallFromGitHub(id string) error {
	plugin, err := in.registry.FetchPlugin(id)
	if err != nil {
		return fmt.Errorf("fetch plugin metadata: %w", err)
	}
	return in.InstallRemote(plugin)
}

// InstallRemote downloads and installs a registry plugin at its selected
// version, marking the install source as market. An empty Version or
// DownloadURL on the RemotePlugin means version selection found nothing
// compatible with this host (INCOMPATIBLE_VERSION).
func (in *Installer) InstallRemote(remote *RemotePlugin) error {
	if remote.Version == "" || remote.DownloadURL == "" {
		return Errorf(remote.ID, ErrIncompatibleVersion,
			"no registry version compatible with host %s", HostVersion)
	}
	zipData, err := in.downloadFile(remote.DownloadURL)
	if err != nil {
		return fmt.Errorf("download plugin archive: %w", err)
	}
	if _, err := in.InstallFromBytes(zipData, SourceMarket); err != nil {
		return fmt.Errorf("install from archive: %w", err)
	}
	return nil
}

// downloadFile downloads a file from the given URL and returns its contents.
func (in *Installer) downloadFile(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")

	resp, err := in.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// Uninstall removes a plugin from the plugins directory.
func (in *Installer) Uninstall(id string) error {
	pluginDir := filepath.Join(in.pluginsRoot, id)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %s not installed", id)
	}

	return os.RemoveAll(pluginDir)
}

// IsInstalled checks if a plugin is currently installed.
func (in *Installer) IsInstalled(id string) bool {
	pluginDir := filepath.Join(in.pluginsRoot, id)
	_, err := os.Stat(pluginDir)
	return err == nil
}

// InstalledVersion returns the version of an installed plugin, or empty string if not installed.
func (in *Installer) InstalledVersion(id string) (string, error) {
	pluginDir := filepath.Join(in.pluginsRoot, id)

	// Check current.json first
	currentPath := filepath.Join(pluginDir, "current.json")
	if data, err := os.ReadFile(currentPath); err == nil {
		var current map[string]string
		if json.Unmarshal(data, &current) == nil {
			if version, ok := current["version"]; ok {
				return version, nil
			}
		}
	}

	// Otherwise, scan version directories
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if e.IsDir() && isValidVersion(e.Name()) {
			return e.Name(), nil
		}
	}

	return "", fmt.Errorf("no valid version found")
}

// isValidVersion checks if a directory name is a valid semantic version.
func isValidVersion(name string) bool {
	// Simple check: version should contain dots (e.g., "0.1.0")
	return strings.Contains(name, ".") && !strings.HasPrefix(name, ".")
}

// SetInstallHook sets a callback function that gets called during installation progress.
func (in *Installer) SetInstallHook(fn func(id, version string)) {
	in.installHook = fn
}

// InstallFromZipFile installs a plugin from a local zip archive — the
// settings "Import zip" path. The archive must contain exactly one
// plugin.json, either at the zip root or inside a leading directory (e.g.
// GitHub "Source code" downloads); every payload entry must live under that
// manifest directory. Entries keep their relative paths (subdirectories
// preserved). The manifest must pass ParseManifest and the declared main
// file must exist, otherwise nothing is written to the install tree.
// source is recorded in current.json (any value but SourceMarket is stored
// as SourceLocal). Returns the parsed manifest of the installed plugin.
func (in *Installer) InstallFromZipFile(zipPath, source string) (*Manifest, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()
	return in.installFromReader(&r.Reader, source)
}

// installFromReader validates and extracts a plugin archive. Extraction
// goes to a staging directory first; the install is promoted via rename
// only after full validation, so a failed import never leaves a half
// version directory behind.
func (in *Installer) installFromReader(r *zip.Reader, source string) (*Manifest, error) {
	if source != SourceMarket {
		source = SourceLocal
	}

	// Locate the manifest: exactly one plugin.json outside metadata junk.
	var manifestFile *zip.File
	for _, f := range r.File {
		if isZipJunk(f.Name) {
			continue
		}
		if path.Base(f.Name) == ManifestFile {
			if manifestFile != nil {
				return nil, fmt.Errorf("multiple %s files in archive", ManifestFile)
			}
			manifestFile = f
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("%s not found in zip", ManifestFile)
	}

	manifestData, err := readZipFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("read manifest in zip: %w", err)
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	// Payload root: the manifest's directory prefix ("" for a root-level
	// manifest).
	root := path.Dir(manifestFile.Name)
	if root == "." {
		root = ""
	} else {
		root += "/"
	}

	if err := os.MkdirAll(in.pluginsRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create plugins root: %w", err)
	}
	staging, err := os.MkdirTemp(in.pluginsRoot, ".import-")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	mainFound := false
	for _, f := range r.File {
		if f.FileInfo().IsDir() || isZipJunk(f.Name) {
			continue
		}
		rel, ok := zipRelPath(root, f.Name)
		if !ok {
			return nil, fmt.Errorf("entry %q escapes or lies outside the plugin root %q", f.Name, root)
		}
		data, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s in zip: %w", f.Name, err)
		}
		target := filepath.Join(staging, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("create directory: %w", err)
		}
		if err := os.WriteFile(target, data, f.Mode().Perm()); err != nil {
			return nil, fmt.Errorf("write %s: %w", rel, err)
		}
		if rel == manifest.Main {
			mainFound = true
		}
	}
	if !mainFound {
		return nil, fmt.Errorf("manifest main %q missing from archive", manifest.Main)
	}

	// Promote: replace any existing same-version install with the staged
	// tree. ID and Version are validated (reverse-domain / SemVer), so both
	// are safe path components.
	dest := filepath.Join(in.pluginsRoot, manifest.ID, manifest.Version)
	if err := os.RemoveAll(dest); err != nil {
		return nil, fmt.Errorf("replace existing version: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("create plugin directory: %w", err)
	}
	if err := os.Rename(staging, dest); err != nil {
		return nil, fmt.Errorf("promote install: %w", err)
	}

	// Pin the installed version (and its source) so ResolveVersionDir
	// selects it even when a higher version is (or becomes) installed.
	// Non-fatal.
	pinData, _ := json.Marshal(currentPin{Version: manifest.Version, Source: source})
	if err := os.WriteFile(filepath.Join(in.pluginsRoot, manifest.ID, CurrentFile), pinData, 0o644); err != nil {
		log.Printf("plugin: pin %s for %s: %v", CurrentFile, manifest.ID, err)
	}
	if in.installHook != nil {
		in.installHook(manifest.ID, manifest.Version)
	}
	return manifest, nil
}

// readZipFile reads one archive entry into memory.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// isZipJunk reports whether a zip entry is archive metadata that must never
// be extracted or considered part of the plugin.
func isZipJunk(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") ||
		name == ".DS_Store" || strings.HasSuffix(name, "/.DS_Store")
}

// zipRelPath maps a zip entry name to its slash path relative to the plugin
// root, rejecting entries outside the root and any path that would climb
// out of the install directory (zip-slip).
func zipRelPath(root, name string) (string, bool) {
	if root != "" && !strings.HasPrefix(name, root) {
		return "", false
	}
	cleaned := path.Clean(strings.TrimPrefix(name, root))
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	return cleaned, true
}

// InstallFromBytes installs a plugin from raw zip bytes.
func (in *Installer) InstallFromBytes(data []byte, source string) (*Manifest, error) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "kyvro-plugin-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}

	// Install from the temp file
	return in.InstallFromZipFile(tmpFile.Name(), source)
}
