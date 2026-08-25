package plugin

import "kyvro/internal/core"

// PluginStorage is the per-plugin string→string key-value store, persisted
// in the shared usage database under the bucket "plugin:<id>". Data survives
// plugin reloads and version upgrades (spec §11).
type PluginStorage struct {
	store *core.Store
	ns    string
}

// NewPluginStorage creates the storage facade for pluginID.
func NewPluginStorage(store *core.Store, pluginID string) *PluginStorage {
	return &PluginStorage{store: store, ns: "plugin:" + pluginID}
}

// Get returns the value for key ("" and false when absent).
func (p *PluginStorage) Get(key string) (string, bool, error) {
	return p.store.GetNS(p.ns, key)
}

// Set stores value under key.
func (p *PluginStorage) Set(key, value string) error {
	return p.store.PutNS(p.ns, key, value)
}

// Delete removes key.
func (p *PluginStorage) Delete(key string) error {
	return p.store.DeleteNS(p.ns, key)
}
