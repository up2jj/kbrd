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
	Name     string            `json:"name"`
	Source   string            `json:"source"`
	Versions []PluginVersion   `json:"versions,omitempty"`
	Channels map[string]string `json:"channels,omitempty"`
}

// PluginVersion maps a published semantic version to a Git revision. Ref may
// be a tag, branch, or commit; resolution always records the resulting full
// commit and content digest in the board lock.
type PluginVersion struct {
	Version string `json:"version"`
	Ref     string `json:"ref"`
	Source  string `json:"source,omitempty"`
}

type PluginManifest struct {
	APIVersion    int          `json:"apiVersion"`
	Name          string       `json:"name"`
	Version       string       `json:"version,omitempty"`
	Description   string       `json:"description"`
	Entrypoint    string       `json:"entrypoint,omitempty"`
	Assets        PluginAssets `json:"assets,omitzero"`
	Author        Owner        `json:"author,omitempty"`
	License       string       `json:"license,omitempty"`
	Homepage      string       `json:"homepage,omitempty"`
	Commands      []string     `json:"commands,omitempty"`
	Hooks         []string     `json:"hooks,omitempty"`
	Layers        []string     `json:"layers,omitempty"`
	Timers        []string     `json:"timers,omitempty"`
	NetworkAccess bool         `json:"networkAccess,omitempty"`
	ShellAccess   bool         `json:"shellAccess,omitempty"`
	README        string       `json:"readme,omitempty"`
	Changelog     string       `json:"changelog,omitempty"`
}

// PluginAssets declares inspectable, non-Lua content shipped by a plugin. Each
// value is a path relative to the plugin root and may name either a regular
// file or a directory. A plugin can combine any of these assets with a Lua
// entrypoint, or omit the entrypoint and be entirely static.
type PluginAssets struct {
	CardTemplates      string `json:"cardTemplates,omitempty"`
	Themes             string `json:"themes,omitempty"`
	FrontmatterPresets string `json:"frontmatterPresets,omitempty"`
	CustomCommands     string `json:"customCommands,omitempty"`
	BoardStarters      string `json:"boardStarters,omitempty"`
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
	ID                string       `json:"id"`
	Disabled          bool         `json:"disabled,omitempty"`
	Version           string       `json:"version,omitempty"`
	RequestedVersion  string       `json:"requestedVersion,omitempty"`
	Channel           string       `json:"channel,omitempty"`
	Description       string       `json:"description,omitempty"`
	Marketplace       string       `json:"marketplace"`
	MarketplaceURL    string       `json:"marketplaceUrl"`
	MarketplaceRef    string       `json:"marketplaceRef,omitempty"`
	MarketplaceCommit string       `json:"marketplaceCommit"`
	Source            string       `json:"source"`
	Entrypoint        string       `json:"entrypoint,omitempty"`
	Assets            PluginAssets `json:"assets,omitzero"`
	ContentSHA256     string       `json:"contentSha256"`
}

type BoardLock struct {
	APIVersion int            `json:"apiVersion"`
	Plugins    []LockedPlugin `json:"plugins"`
	History    []LockedPlugin `json:"history,omitempty"`
}

// UpdateOptions controls how an update selects a published version. An empty
// channel keeps the selection policy already recorded in the lock.
type UpdateOptions struct {
	Channel string
}

// RuntimePlugin is a verified cached plugin ready for the Lua host.
type RuntimePlugin struct {
	ID         string
	Root       string
	ModuleRoot string
	Entrypoint string
}

// RuntimeAssets contains verified absolute paths for one enabled plugin's
// static content. Empty fields correspond to asset kinds it does not ship.
type RuntimeAssets struct {
	ID                 string
	Root               string
	CardTemplates      string
	Themes             string
	FrontmatterPresets string
	CustomCommands     string
	BoardStarters      string
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

// UpdatePreview describes a newer marketplace resolution without changing the
// board lock or activating plugin content.
type UpdatePreview struct {
	ID              string
	Current         LockedPlugin
	Candidate       LockedPlugin
	ManifestChanges []ManifestChange
	Files           []PluginFileChange
	Patch           string
}

// Outdated reports whether applying a preview would change the board lock.
func (p UpdatePreview) Outdated() bool {
	return p.Current != p.Candidate
}

// ManifestChange describes one changed declarative plugin.json field.
type ManifestChange struct {
	Field  string
	Before string
	After  string
}

// PluginFileChange identifies one changed path in a candidate plugin tree.
type PluginFileChange struct {
	Path   string
	Status string
}
