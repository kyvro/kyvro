package plugin

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Installer handles downloading and installing plugins from the registry.
type Installer struct {
	client       *http.Client
	pluginsRoot  string
	registry     *RegistryClient
	installHook  func(id, version string) // Callback for UI updates
}

// NewInstaller creates a plugin installer for the given plugins directory.
func NewInstaller(pluginsRoot string) *Installer {
	return &Installer{
		client:      &http.Client{Timeout: 30 * time.Second},
		pluginsRoot: pluginsRoot,
		registry:    NewRegistryClient(),
	}
}

// InstallFromGitHub installs a plugin directly from the GitHub plugins repository.
// It downloads the plugin zip archive and installs it to the plugins directory.
func (in *Installer) InstallFromGitHub(id string) error {
	// Fetch plugin metadata from registry
	plugin, err := in.registry.FetchPlugin(id)
	if err != nil {
		return fmt.Errorf("fetch plugin metadata: %w", err)
	}

	// Download plugin zip archive
	zipData, err := in.downloadFile(plugin.DownloadURL)
	if err != nil {
		return fmt.Errorf("download plugin archive: %w", err)
	}

	// Install from the downloaded zip data
	if err := in.InstallFromBytes(zipData); err != nil {
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

// InstallFromZipFile installs a plugin from a local zip file.
func (in *Installer) InstallFromZipFile(zipPath string) error {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	// Find plugin.json to determine plugin ID and version
	var manifest *Manifest
	var manifestFile *zip.File
	for _, f := range r.File {
		if filepath.Base(f.Name) == "plugin.json" {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		return fmt.Errorf("plugin.json not found in zip")
	}

	// Read manifest
	rc, err := manifestFile.Open()
	if err != nil {
		return fmt.Errorf("open manifest in zip: %w", err)
	}
	manifestData, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("read manifest in zip: %w", err)
	}

	manifest = &Manifest{}
	if err := json.Unmarshal(manifestData, manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	// Create target directory
	targetDir := filepath.Join(in.pluginsRoot, manifest.ID, manifest.Version)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	// Extract all files
	for _, f := range r.File {
		// Skip directory entries and macOS metadata
		if f.FileInfo().IsDir() || strings.Contains(f.Name, "__MACOSX") {
			continue
		}

		// Calculate target path (flatten to target directory)
		targetPath := filepath.Join(targetDir, filepath.Base(f.Name))

		// Create file
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open file in zip: %w", err)
		}

		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("read file in zip: %w", err)
		}

		if err := os.WriteFile(targetPath, data, f.Mode()); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
	}

	// Create current.json to pin this version
	currentPath := filepath.Join(in.pluginsRoot, manifest.ID, "current.json")
	currentData := map[string]string{"version": manifest.Version}
	currentJSON, _ := json.Marshal(currentData)
	if err := os.WriteFile(currentPath, currentJSON, 0o644); err != nil {
		// Non-fatal
	}

	return nil
}

// InstallFromBytes installs a plugin from raw zip bytes.
func (in *Installer) InstallFromBytes(data []byte) error {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "kyvro-plugin-*.zip")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Install from the temp file
	return in.InstallFromZipFile(tmpFile.Name())
}
