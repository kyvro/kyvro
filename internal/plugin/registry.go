package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// RegistryList represents the structure of list.json from the plugin registry.
type RegistryList struct {
	Version      int       `json:"version"`
	LastUpdated  time.Time `json:"lastUpdated"`
	Plugins      []RegistryPlugin `json:"plugins"`
}

// RegistryPlugin represents a plugin entry in the registry list.json.
type RegistryPlugin struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Version        string            `json:"version"`
	Author         Author            `json:"author"`
	Repository     string            `json:"repository"`
	Homepage       string            `json:"homepage"`
	MinHostVersion string            `json:"minHostVersion"`
	Permissions    []string          `json:"permissions"`
	Platforms      []string          `json:"platforms"`
	Versions       []string          `json:"versions"`
	Category       string            `json:"category"`
	Keywords       []string          `json:"keywords"`
	Stats          PluginStats       `json:"stats"`
}

// PluginStats represents plugin statistics.
type PluginStats struct {
	Downloads int     `json:"downloads"`
	Rating    float64 `json:"rating"`
}

// RegistryClient fetches plugin metadata from a remote registry (e.g., GitHub).
type RegistryClient struct {
	client   *http.Client
	registry string // Base URL for raw content: "https://raw.githubusercontent.com/kyvro/plugins/main"
	listURL  string // URL for list.json
}

// NewRegistryClient creates a client for the official Kyvro plugin registry.
func NewRegistryClient() *RegistryClient {
	const baseURL = "https://raw.githubusercontent.com/kyvro/plugins/main"
	return &RegistryClient{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		registry: baseURL,
		listURL:  baseURL + "/list.json",
	}
}

// RemotePlugin represents a plugin available for installation from the registry.
type RemotePlugin struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Description    string            `json:"description"`
	Author         Author            `json:"author"`
	DownloadURL    string            `json:"download_url"`
	IconURL        string            `json:"icon_url,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at,omitempty"`
	MinHostVersion string            `json:"minHostVersion"`
	Permissions    []string          `json:"permissions"`
	Platforms      []string          `json:"platforms"`
	Category       string            `json:"category"`
	Keywords       []string          `json:"keywords"`
	Stats          PluginStats       `json:"stats"`
}

// FetchPlugins retrieves the list of available plugins from the registry.
// It first checks the lastUpdated timestamp to decide if the full list needs to be fetched.
func (r *RegistryClient) FetchPlugins() ([]RemotePlugin, error) {
	// First, try to get the remote lastUpdated timestamp
	remoteLastUpdated, err := r.fetchLastUpdated()
	if err != nil {
		fmt.Printf("DEBUG: Failed to fetch lastUpdated, falling back to full list: %v\n", err)
		return r.fetchFullList()
	}

	// Get the local cached lastUpdated timestamp
	localLastUpdated := r.getLocalCachedLastUpdated()
	fmt.Printf("DEBUG: Remote lastUpdated: %s, Local cached: %s\n", remoteLastUpdated, localLastUpdated)

	// If timestamps match, use the cached list
	if remoteLastUpdated == localLastUpdated && localLastUpdated != "" {
		fmt.Printf("DEBUG: Timestamps match, using cached list\n")
		return r.getCachedList()
	}

	// Timestamps differ or no cache, fetch the full list
	fmt.Printf("DEBUG: Timestamps differ or no cache, fetching full list\n")
	return r.fetchFullList()
}

// fetchLastUpdated retrieves the lastUpdated timestamp from the server.
func (r *RegistryClient) fetchLastUpdated() (string, error) {
	// Construct the lastUpdated file URL
	lastUpdatedURL := r.registry + "/lastUpdated"

	req, err := http.NewRequest("GET", lastUpdatedURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read the timestamp (single line)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// getLocalCachedLastUpdated retrieves the lastUpdated timestamp from the local cache.
func (r *RegistryClient) getLocalCachedLastUpdated() string {
	// Try to read from local cache directory
	cachePath := r.getLocalCachePath()
	if data, err := os.ReadFile(cachePath + "/lastUpdated"); err == nil {
		return string(data)
	}
	return ""
}

// getCachedList retrieves the plugin list from local cache.
func (r *RegistryClient) getCachedList() ([]RemotePlugin, error) {
	cachePath := r.getLocalCachePath()
	listPath := cachePath + "/list.json"

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, fmt.Errorf("read cached list: %w", err)
	}

	var list RegistryList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse cached list: %w", err)
	}

	return r.convertToList(&list)
}

// getLocalCachePath returns the path to the local plugin cache directory.
func (r *RegistryClient) getLocalCachePath() string {
	// Use the same directory structure as the plugin system
	homeDir, _ := os.UserHomeDir()
	cachePath := filepath.Join(homeDir, "Library", "Application Support", "Kyvro")

	// Ensure cache directory exists
	os.MkdirAll(cachePath, 0o755)

	return cachePath
}

// fetchFullList retrieves the complete plugin list from the server.
func (r *RegistryClient) fetchFullList() ([]RemotePlugin, error) {
	var list RegistryList

	// Try HTTP fetch first
	if r.listURL != "" {
		req, err := http.NewRequest("GET", r.listURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")

			resp, err := r.client.Do(req)
			if err == nil {
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					err = json.NewDecoder(resp.Body).Decode(&list)
					if err == nil {
						fmt.Printf("DEBUG: Successfully fetched %d plugins from HTTP\n", len(list.Plugins))

						// Cache the result locally
						r.cacheList(&list)

						return r.convertToList(&list)
					} else {
						fmt.Printf("DEBUG: Failed to decode HTTP response: %v\n", err)
					}
				} else {
					fmt.Printf("DEBUG: HTTP request returned status %d\n", resp.StatusCode)
				}
			} else {
				fmt.Printf("DEBUG: HTTP request failed: %v\n", err)
			}
		} else {
			fmt.Printf("DEBUG: Failed to create HTTP request: %v\n", err)
		}
	}

	// Fallback to local file (for development/testing)
	fmt.Printf("DEBUG: Falling back to local file\n")
	var err error
	list, err = r.fetchLocalList()
	if err != nil {
		fmt.Printf("DEBUG: Local file fetch failed: %v\n", err)
		return nil, fmt.Errorf("fetch registry: %w", err)
	}

	fmt.Printf("DEBUG: Successfully loaded %d plugins from local file\n", len(list.Plugins))
	return r.convertToList(&list)
}

// cacheList saves the plugin list and lastUpdated timestamp to local cache.
func (r *RegistryClient) cacheList(list *RegistryList) error {
	cachePath := r.getLocalCachePath()

	// Cache the list.json
	listData, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal list: %w", err)
	}

	if err := os.WriteFile(cachePath+"/list.json", listData, 0o644); err != nil {
		return fmt.Errorf("write cached list: %w", err)
	}

	// Cache the lastUpdated timestamp
	lastUpdated := list.LastUpdated.Format("2006-01-02T15:04:05Z")
	if err := os.WriteFile(cachePath+"/lastUpdated", []byte(lastUpdated), 0o644); err != nil {
		return fmt.Errorf("write cached lastUpdated: %w", err)
	}

	fmt.Printf("DEBUG: Cached plugin list with timestamp: %s\n", lastUpdated)
	return nil
}

func (r *RegistryClient) fetchLocalList() (RegistryList, error) {
	var list RegistryList

	// Try to find list.json in local plugins directory
	paths := []string{
		"./plugins/list.json",
		"../plugins/list.json",
		"../../plugins/list.json",
	}

	fmt.Printf("DEBUG: Trying to load local list.json from paths: %v\n", paths)
	for _, path := range paths {
		fmt.Printf("DEBUG: Trying path: %s\n", path)
		if data, err := os.ReadFile(path); err == nil {
			fmt.Printf("DEBUG: Successfully read file from %s\n", path)
			if err := json.Unmarshal(data, &list); err == nil {
				fmt.Printf("DEBUG: Successfully parsed JSON with %d plugins\n", len(list.Plugins))
				return list, nil
			} else {
				fmt.Printf("DEBUG: Failed to parse JSON: %v\n", err)
			}
		} else {
			fmt.Printf("DEBUG: Failed to read file: %v\n", err)
		}
	}

	fmt.Printf("DEBUG: No local list.json found in any path\n")
	return list, fmt.Errorf("no local list.json found")
}

func (r *RegistryClient) convertToList(list *RegistryList) ([]RemotePlugin, error) {
	fmt.Printf("DEBUG: convertToList called with %d plugins\n", len(list.Plugins))
	// Convert registry plugins to remote plugins
	plugins := make([]RemotePlugin, 0, len(list.Plugins))
	for _, rp := range list.Plugins {
		fmt.Printf("DEBUG: Processing plugin: %s (version %s)\n", rp.ID, rp.Version)

		// Generate download URL for the latest version
		// Format: https://raw.githubusercontent.com/kyvro/plugins/main/{plugin-id}/{plugin-id}-{version}.zip
		if len(rp.Versions) == 0 {
			fmt.Printf("DEBUG: Skipping plugin %s - no versions available\n", rp.ID)
			continue
		}

		latestVersion := rp.Version // Use the version field as latest
		downloadURL := fmt.Sprintf("https://raw.githubusercontent.com/kyvro/plugins/main/%s/%s-%s.zip",
			rp.ID, rp.ID, latestVersion)

		plugin := RemotePlugin{
			ID:             rp.ID,
			Name:           rp.Name,
			Version:        rp.Version,
			Description:    rp.Description,
			Author:         rp.Author,
			DownloadURL:    downloadURL,
			UpdatedAt:      list.LastUpdated,
			MinHostVersion:  rp.MinHostVersion,
			Permissions:    rp.Permissions,
			Platforms:      rp.Platforms,
			Category:       rp.Category,
			Keywords:       rp.Keywords,
			Stats:          rp.Stats,
		}

		fmt.Printf("DEBUG: Created plugin entry: %s with download URL %s\n", plugin.ID, plugin.DownloadURL)
		plugins = append(plugins, plugin)
	}

	fmt.Printf("DEBUG: Returning %d plugins from convertToList\n", len(plugins))
	return plugins, nil
}

// FetchPlugin retrieves metadata for a specific plugin by ID.
func (r *RegistryClient) FetchPlugin(id string) (*RemotePlugin, error) {
	var list RegistryList
	var err error

	// Try HTTP fetch first
	if r.listURL != "" {
		req, err := http.NewRequest("GET", r.listURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")

			resp, err := r.client.Do(req)
			if err == nil {
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					err = json.NewDecoder(resp.Body).Decode(&list)
					if err == nil {
						return r.findPluginInList(&list, id)
					}
				}
			}
		}
	}

	// Fallback to local file
	list, err = r.fetchLocalList()
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}

	return r.findPluginInList(&list, id)
}

func (r *RegistryClient) findPluginInList(list *RegistryList, id string) (*RemotePlugin, error) {
	// Find the specific plugin
	for _, rp := range list.Plugins {
		if rp.ID == id {
			if len(rp.Versions) == 0 {
				return nil, fmt.Errorf("plugin %s has no versions available", id)
			}

			// Use the version field as latest
			latestVersion := rp.Version
			downloadURL := fmt.Sprintf("https://raw.githubusercontent.com/kyvro/plugins/main/%s/%s-%s.zip",
				rp.ID, rp.ID, latestVersion)

			return &RemotePlugin{
				ID:             rp.ID,
				Name:           rp.Name,
				Version:        rp.Version,
				Description:    rp.Description,
				Author:         rp.Author,
				DownloadURL:    downloadURL,
				UpdatedAt:      list.LastUpdated,
				MinHostVersion:  rp.MinHostVersion,
				Permissions:    rp.Permissions,
				Platforms:      rp.Platforms,
				Category:       rp.Category,
				Keywords:       rp.Keywords,
				Stats:          rp.Stats,
			}, nil
		}
	}

	return nil, fmt.Errorf("plugin %s not found in registry", id)
}
