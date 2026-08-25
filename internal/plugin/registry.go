package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RegistryClient fetches plugin metadata from a remote registry (e.g., GitHub).
type RegistryClient struct {
	client   *http.Client
	registry string // Base URL for raw content: "https://raw.githubusercontent.com/kyvro/kyvro/main"
}

// NewRegistryClient creates a client for the official Kyvro plugin registry.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		registry: "https://raw.githubusercontent.com/kyvro/kyvro/main",
	}
}

// RemotePlugin represents a plugin available for installation from the registry.
type RemotePlugin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	DownloadURL string    `json:"download_url"` // URL to download plugin archive
	IconURL     string    `json:"icon_url"`     // URL to plugin icon
	UpdatedAt   time.Time `json:"updated_at"`
}

// FetchPlugins retrieves the list of available plugins from the registry.
// It queries the GitHub API for the plugins-official directory structure.
func (r *RegistryClient) FetchPlugins() ([]RemotePlugin, error) {
	// Use GitHub API to list plugin directories
	url := "https://api.github.com/repos/kyvro/kyvro/contents/plugins-official"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add user agent to avoid rate limiting issues
	req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}

	var contents []struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		URL        string `json:"url"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}

	// Fetch manifest for each plugin directory
	plugins := make([]RemotePlugin, 0, len(contents))
	for _, item := range contents {
		if item.Type != "dir" {
			continue
		}

		// Skip non-plugin directories (like README)
		if item.Name == "README.md" || item.Name == ".gitignore" {
			continue
		}

		// Fetch plugin manifest to get metadata using raw.githubusercontent.com
		manifestURL := fmt.Sprintf("%s/plugins-official/%s/plugin.json", r.registry, item.Name)
		manifestResp, err := r.client.Get(manifestURL)
		if err != nil {
			continue // Skip plugins we can't fetch
		}

		if manifestResp.StatusCode != http.StatusOK {
			manifestResp.Body.Close()
			continue
		}

		manifestData, err := io.ReadAll(manifestResp.Body)
		manifestResp.Body.Close()
		if err != nil {
			continue
		}

		var manifest Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			continue
		}

		// Create remote plugin entry
		plugin := RemotePlugin{
			ID:          manifest.ID,
			Name:        manifest.Name,
			Version:     manifest.Version,
			Description: manifest.Description,
			Author:      manifest.Author.Name,
			DownloadURL: fmt.Sprintf("%s/plugins-official/%s", r.registry, item.Name),
			UpdatedAt:   time.Now(),
		}

		// Add icon URL if available using raw.githubusercontent.com
		if manifest.Icon != "" {
			plugin.IconURL = fmt.Sprintf("%s/plugins-official/%s/%s", r.registry, item.Name, manifest.Icon)
		}

		plugins = append(plugins, plugin)
	}

	return plugins, nil
}

// FetchPlugin retrieves metadata for a specific plugin by ID.
func (r *RegistryClient) FetchPlugin(id string) (*RemotePlugin, error) {
	plugins, err := r.FetchPlugins()
	if err != nil {
		return nil, err
	}

	for _, p := range plugins {
		if p.ID == id {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("plugin %s not found in registry", id)
}
