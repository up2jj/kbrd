// Package plugin manages Git-backed Lua plugin marketplaces and board-local
// plugin lock files. Installed content is a disposable, verified cache; a
// board's kbrd.plugins.lock is the only activation state.
package plugin

const (
	MarketplaceFile = "marketplace.json"
	PluginFile      = "plugin.json"
	LockFile        = "kbrd.plugins.lock"
	APIVersion      = 1
)

type Owner struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type MarketplaceManifest struct {
	APIVersion  int                `json:"apiVersion"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Owner       Owner              `json:"owner,omitempty"`
	Plugins     []MarketplaceEntry `json:"plugins"`
}

type MarketplaceEntry struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type PluginManifest struct {
	APIVersion    int      `json:"apiVersion"`
	Name          string   `json:"name"`
	Version       string   `json:"version,omitempty"`
	Description   string   `json:"description"`
	Entrypoint    string   `json:"entrypoint"`
	Author        Owner    `json:"author,omitempty"`
	License       string   `json:"license,omitempty"`
	Homepage      string   `json:"homepage,omitempty"`
	Commands      []string `json:"commands,omitempty"`
	Hooks         []string `json:"hooks,omitempty"`
	Layers        []string `json:"layers,omitempty"`
	Timers        []string `json:"timers,omitempty"`
	NetworkAccess bool     `json:"networkAccess,omitempty"`
	ShellAccess   bool     `json:"shellAccess,omitempty"`
	README        string   `json:"readme,omitempty"`
	Changelog     string   `json:"changelog,omitempty"`
}

type Marketplace struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit"`
	Description string `json:"description,omitempty"`
}

type Registry struct {
	APIVersion   int           `json:"apiVersion"`
	Marketplaces []Marketplace `json:"marketplaces"`
}

type LockedPlugin struct {
	ID                string `json:"id"`
	Version           string `json:"version,omitempty"`
	Description       string `json:"description,omitempty"`
	Marketplace       string `json:"marketplace"`
	MarketplaceURL    string `json:"marketplaceUrl"`
	MarketplaceRef    string `json:"marketplaceRef,omitempty"`
	MarketplaceCommit string `json:"marketplaceCommit"`
	Source            string `json:"source"`
	Entrypoint        string `json:"entrypoint"`
	ContentSHA256     string `json:"contentSha256"`
}

type BoardLock struct {
	APIVersion int            `json:"apiVersion"`
	Plugins    []LockedPlugin `json:"plugins"`
}

// RuntimePlugin is a verified cached plugin ready for the Lua host.
type RuntimePlugin struct {
	ID         string
	Root       string
	ModuleRoot string
	Entrypoint string
}

type AvailablePlugin struct {
	ID          string
	Version     string
	Description string
}

// PluginInfo is declarative marketplace and board-lock metadata. Constructing
// it never loads or executes a plugin entrypoint.
type PluginInfo struct {
	ID          string
	Manifest    PluginManifest
	Marketplace Marketplace
	Installed   *LockedPlugin
}
