package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// registryListVersion is the only list.json schema this client understands
// (slim metadata + versions/minVersions parallel arrays,
// docs/plugin-marketplace.md); any other version is rejected.
const registryListVersion = 1

// registryBaseURL is the raw-content root of the official plugin registry.
const registryBaseURL = "https://raw.githubusercontent.com/kyvro/plugins/main"

// RegistryList is the decoded list.json.
type RegistryList struct {
	Version     int              `json:"version"`
	LastUpdated time.Time        `json:"lastUpdated"`
	Plugins     []RegistryPlugin `json:"plugins"`
}

// RegistryPlugin is one list.json entry. Versions is an ascending SemVer
// list; MinVersions is a parallel array whose i-th element is the minimum
// Kyvro version able to run Versions[i] (docs/plugin-marketplace.md
// 「版本适配」).
type RegistryPlugin struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      Author   `json:"author"`
	Permissions []string `json:"permissions"`
	Platforms   []string `json:"platforms"`
	Versions    []string `json:"versions"`
	MinVersions []string `json:"minVersions"`
}

// RemotePlugin is the client-facing view of a registry plugin after version
// selection. Version is the newest registry version this host can run —
// empty means none is compatible (DownloadURL is empty too), so the UI can
// surface the entry as Incompatible instead of hiding it.
type RemotePlugin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Author      Author    `json:"author"`
	DownloadURL string    `json:"download_url"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	Permissions []string  `json:"permissions"`
	Platforms   []string  `json:"platforms"`
}

// SelectVersion applies the marketplace selection rule: the highest
// versions[i] whose minVersions[i] is satisfied by appVersion. versions /
// minVersions must be equal-length arrays; malformed entries are skipped.
// Returns INCOMPATIBLE_VERSION when no version fits.
func SelectVersion(versions, minVersions []string, appVersion string) (string, error) {
	if len(versions) == 0 {
		return "", Errorf("", ErrInvalidArgument, "registry entry has no versions")
	}
	if len(minVersions) != len(versions) {
		return "", Errorf("", ErrInvalidArgument,
			"minVersions length %d does not match versions length %d", len(minVersions), len(versions))
	}
	best := ""
	for i, v := range versions {
		if !validSemver(v) || !validSemver(minVersions[i]) {
			continue
		}
		if semver.Compare("v"+appVersion, "v"+minVersions[i]) >= 0 &&
			(best == "" || semver.Compare("v"+v, "v"+best) > 0) {
			best = v
		}
	}
	if best == "" {
		return "", Errorf("", ErrIncompatibleVersion,
			"host %s satisfies none of the available versions %v", appVersion, versions)
	}
	return best, nil
}

// decodeList parses list.json bytes and rejects schemas this client cannot
// interpret (the registry launches directly on the final schema).
func decodeList(data []byte) (*RegistryList, error) {
	var list RegistryList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse list.json: %w", err)
	}
	if list.Version != registryListVersion {
		return nil, fmt.Errorf("list.json version %d unsupported (client supports %d)",
			list.Version, registryListVersion)
	}
	return &list, nil
}

// downloadURL builds the raw-GitHub archive URL for one plugin version
// (docs/plugin-marketplace.md「下载 URL 拼接规则」).
func downloadURL(id, version string) string {
	return fmt.Sprintf("%s/%s/%s-%s.zip", registryBaseURL, id, id, version)
}

// remoteFromEntry converts one registry entry into the client view by
// running the selection rule against the running host. Incompatible entries
// keep empty Version/DownloadURL.
func remoteFromEntry(rp *RegistryPlugin, updatedAt time.Time) RemotePlugin {
	version, err := SelectVersion(rp.Versions, rp.MinVersions, HostVersion)
	url := ""
	if err == nil {
		url = downloadURL(rp.ID, version)
	}
	return RemotePlugin{
		ID:          rp.ID,
		Name:        rp.Name,
		Version:     version,
		Description: rp.Description,
		Author:      rp.Author,
		DownloadURL: url,
		UpdatedAt:   updatedAt,
		Permissions: rp.Permissions,
		Platforms:   rp.Platforms,
	}
}

// remoteFromList converts a decoded registry list, ordered by id for
// deterministic UI display.
func remoteFromList(list *RegistryList) []RemotePlugin {
	plugins := make([]RemotePlugin, 0, len(list.Plugins))
	for i := range list.Plugins {
		plugins = append(plugins, remoteFromEntry(&list.Plugins[i], list.LastUpdated))
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
	return plugins
}

// RegistryClient fetches plugin metadata from the remote registry.
type RegistryClient struct {
	client   *http.Client
	registry string // raw-content base URL
	listURL  string // URL of list.json
	cacheDir string // list cache override (tests); "" = app data dir
}

// NewRegistryClient creates a client for the official Kyvro plugin registry.
func NewRegistryClient() *RegistryClient {
	return newRegistryClient(registryBaseURL)
}

// newRegistryClient targets an arbitrary base URL (test injection).
func newRegistryClient(baseURL string) *RegistryClient {
	return &RegistryClient{
		client:   &http.Client{Timeout: 10 * time.Second},
		registry: baseURL,
		listURL:  baseURL + "/list.json",
	}
}

// FetchPlugins retrieves the available plugin list, using the lastUpdated
// file for incremental updates: the full list.json is only downloaded when
// the remote timestamp differs from the locally cached one.
func (r *RegistryClient) FetchPlugins() ([]RemotePlugin, error) {
	remoteStamp, err := r.fetchLastUpdated()
	if err != nil {
		log.Printf("registry: lastUpdated check failed, fetching full list: %v", err)
		return r.fetchFullList()
	}
	if localStamp := r.getLocalCachedLastUpdated(); localStamp != "" && localStamp == remoteStamp {
		plugins, err := r.getCachedList()
		if err == nil {
			return plugins, nil
		}
		// Stale or broken cache (e.g. an old schema): fall through to a
		// full fetch, which rewrites the cache.
		log.Printf("registry: cached list unusable, fetching full list: %v", err)
	}
	return r.fetchFullList()
}

// FetchPlugin retrieves the client view of one registry plugin by ID. The
// remote list is consulted first; the repository-local plugins/list.json is
// a development-only fallback when the network fetch fails.
func (r *RegistryClient) FetchPlugin(id string) (*RemotePlugin, error) {
	if data, err := r.httpGet(r.listURL); err == nil {
		list, derr := decodeList(data)
		if derr != nil {
			return nil, fmt.Errorf("fetch registry: %w", derr)
		}
		if rp := findPluginInList(list, id); rp != nil {
			return rp, nil
		}
		return nil, fmt.Errorf("plugin %s not found in registry", id)
	}
	list, err := r.fetchLocalList()
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	if rp := findPluginInList(&list, id); rp != nil {
		return rp, nil
	}
	return nil, fmt.Errorf("plugin %s not found in registry", id)
}

func findPluginInList(list *RegistryList, id string) *RemotePlugin {
	for i := range list.Plugins {
		if list.Plugins[i].ID == id {
			p := remoteFromEntry(&list.Plugins[i], list.LastUpdated)
			return &p
		}
	}
	return nil
}

// httpGet performs one GET and returns the body on HTTP 200.
func (r *RegistryClient) httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kyvro-Launcher/0.1.0")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// fetchLastUpdated reads the remote lastUpdated stamp (single ISO 8601 line).
func (r *RegistryClient) fetchLastUpdated() (string, error) {
	data, err := r.httpGet(r.registry + "/lastUpdated")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// fetchFullList downloads the complete list.json, caching it on success;
// when that fails it falls back to the repository-local plugins/list.json.
func (r *RegistryClient) fetchFullList() ([]RemotePlugin, error) {
	data, err := r.httpGet(r.listURL)
	if err == nil {
		var list *RegistryList
		list, err = decodeList(data)
		if err == nil {
			if cerr := r.cacheList(list); cerr != nil {
				log.Printf("registry: cache list: %v", cerr)
			}
			return remoteFromList(list), nil
		}
	}
	log.Printf("registry: fetch %s failed (%v), falling back to local list", r.listURL, err)

	local, lerr := r.fetchLocalList()
	if lerr != nil {
		return nil, fmt.Errorf("fetch registry: %w", lerr)
	}
	return remoteFromList(&local), nil
}

// fetchLocalList reads a development list.json from the working directory's
// plugins/ tree (used only when the network is unreachable).
func (r *RegistryClient) fetchLocalList() (RegistryList, error) {
	var list RegistryList
	for _, p := range []string{"plugins/list.json", "../plugins/list.json", "../../plugins/list.json"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		decoded, derr := decodeList(data)
		if derr != nil {
			log.Printf("registry: local %s: %v", p, derr)
			continue
		}
		return *decoded, nil
	}
	return list, fmt.Errorf("no readable list.json found in local plugins/ tree")
}

// getLocalCachedLastUpdated reads the cached lastUpdated stamp.
func (r *RegistryClient) getLocalCachedLastUpdated() string {
	data, err := os.ReadFile(filepath.Join(r.getLocalCachePath(), "lastUpdated"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// getCachedList converts the locally cached list.json into the client view.
func (r *RegistryClient) getCachedList() ([]RemotePlugin, error) {
	data, err := os.ReadFile(filepath.Join(r.getLocalCachePath(), "list.json"))
	if err != nil {
		return nil, fmt.Errorf("read cached list: %w", err)
	}
	list, err := decodeList(data)
	if err != nil {
		return nil, fmt.Errorf("parse cached list: %w", err)
	}
	return remoteFromList(list), nil
}

// cacheList stores list.json and its lastUpdated stamp for the incremental
// check on the next startup.
func (r *RegistryClient) cacheList(list *RegistryList) error {
	dir := r.getLocalCachePath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal list: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), data, 0o644); err != nil {
		return fmt.Errorf("write cached list: %w", err)
	}
	stamp := []byte(list.LastUpdated.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "lastUpdated"), stamp, 0o644); err != nil {
		return fmt.Errorf("write cached lastUpdated: %w", err)
	}
	return nil
}

// getLocalCachePath is the directory shared with the rest of the app data.
func (r *RegistryClient) getLocalCachePath() string {
	if r.cacheDir != "" {
		return r.cacheDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Library", "Application Support", "Kyvro")
}
