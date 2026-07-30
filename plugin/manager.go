package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	kbrdfs "kbrd/fs"
)

type Manager struct {
	Paths Paths
}

func NewManager(paths Paths) *Manager { return &Manager{Paths: paths} }

func DefaultManager() (*Manager, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	return NewManager(paths), nil
}

func (m *Manager) Marketplaces() ([]Marketplace, error) {
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return nil, err
	}
	return slices.Clone(registry.Marketplaces), nil
}

func (m *Manager) Search(query string) ([]AvailablePlugin, error) {
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var found []AvailablePlugin
	for _, marketplace := range registry.Marketplaces {
		root := filepath.Join(m.Paths.MarketplaceCache, marketplace.Name)
		manifest, err := LoadMarketplace(root)
		if err != nil {
			return nil, fmt.Errorf("load marketplace %s: %w", marketplace.Name, err)
		}
		for _, entry := range manifest.Plugins {
			source, _ := safeRelativePath(root, entry.Source)
			pluginManifest, err := LoadPlugin(source)
			if err != nil {
				return nil, err
			}
			id := marketplace.Name + "/" + entry.Name
			haystack := strings.ToLower(id + " " + pluginManifest.Description)
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			found = append(found, AvailablePlugin{ID: id, Version: pluginManifest.Version, Description: pluginManifest.Description})
		}
	}
	slices.SortFunc(found, func(a, b AvailablePlugin) int { return cmpString(a.ID, b.ID) })
	return found, nil
}

// Info returns the metadata declared by a marketplace plugin and its optional
// board lock entry. It reads manifests only and never initializes the Lua host.
func (m *Manager) Info(boardDir, id string) (PluginInfo, error) {
	marketName, pluginName, ok := splitID(id)
	if !ok {
		return PluginInfo{}, fmt.Errorf("plugin must be qualified as marketplace/plugin")
	}
	marketplace, repo, err := m.marketplace(marketName)
	if err != nil {
		return PluginInfo{}, err
	}
	marketplaceManifest, err := LoadMarketplace(repo)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("load marketplace %s: %w", marketName, err)
	}
	entry, ok := marketplaceEntry(marketplaceManifest, pluginName)
	if !ok {
		return PluginInfo{}, fmt.Errorf("marketplace %q has no plugin %q", marketName, pluginName)
	}
	source, err := safeRelativePath(repo, entry.Source)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("plugin %s source: %w", id, err)
	}
	manifest, err := LoadPlugin(source)
	if err != nil {
		return PluginInfo{}, fmt.Errorf("load plugin %s: %w", id, err)
	}

	info := PluginInfo{ID: id, Manifest: manifest, Marketplace: marketplace}
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return PluginInfo{}, err
	}
	if i := slices.IndexFunc(lock.Plugins, func(locked LockedPlugin) bool { return locked.ID == id }); i >= 0 {
		installed := lock.Plugins[i]
		info.Installed = &installed
	}
	return info, nil
}

func (m *Manager) AddMarketplace(ctx context.Context, rawURL, ref string) (Marketplace, error) {
	return m.addMarketplace(ctx, rawURL, ref, "")
}

func (m *Manager) addMarketplace(ctx context.Context, rawURL, ref, expectedName string) (Marketplace, error) {
	normalizedURL, err := normalizeGitURL(rawURL)
	if err != nil {
		return Marketplace{}, err
	}
	rawURL = normalizedURL
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return Marketplace{}, err
	}
	if err := os.MkdirAll(m.Paths.MarketplaceCache, 0o700); err != nil {
		return Marketplace{}, fmt.Errorf("create marketplace cache: %w", err)
	}
	tmp, err := os.MkdirTemp(m.Paths.MarketplaceCache, ".add-*")
	if err != nil {
		return Marketplace{}, fmt.Errorf("create marketplace staging directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	repo := filepath.Join(tmp, "repo")
	if err := kbrdfs.GitCloneContext(ctx, rawURL, repo); err != nil {
		return Marketplace{}, fmt.Errorf("clone marketplace: %w", err)
	}
	commit, err := m.checkoutLatest(ctx, repo, ref)
	if err != nil {
		return Marketplace{}, fmt.Errorf("resolve marketplace revision: %w", err)
	}
	manifest, err := LoadMarketplace(repo)
	if err != nil {
		return Marketplace{}, err
	}
	if expectedName != "" && manifest.Name != expectedName {
		return Marketplace{}, fmt.Errorf("marketplace lock expects name %q, but repository declares %q", expectedName, manifest.Name)
	}
	if slices.ContainsFunc(registry.Marketplaces, func(existing Marketplace) bool { return existing.Name == manifest.Name }) {
		return Marketplace{}, fmt.Errorf("marketplace %q is already registered", manifest.Name)
	}
	destination := filepath.Join(m.Paths.MarketplaceCache, manifest.Name)
	if _, err := os.Stat(destination); err == nil {
		return Marketplace{}, fmt.Errorf("marketplace cache path already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Marketplace{}, err
	}
	if err := os.Rename(repo, destination); err != nil {
		return Marketplace{}, fmt.Errorf("activate marketplace cache: %w", err)
	}
	marketplace := Marketplace{
		Name: manifest.Name, URL: rawURL, Ref: ref, Commit: commit,
		Description: manifest.Description,
	}
	registry.Marketplaces = append(registry.Marketplaces, marketplace)
	if err := saveRegistry(m.Paths, registry); err != nil {
		_ = os.RemoveAll(destination)
		return Marketplace{}, err
	}
	return marketplace, nil
}

func (m *Manager) UpdateMarketplaces(ctx context.Context, name string) ([]Marketplace, error) {
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return nil, err
	}
	var updated []Marketplace
	found := name == ""
	for i := range registry.Marketplaces {
		marketplace := &registry.Marketplaces[i]
		if name != "" && marketplace.Name != name {
			continue
		}
		found = true
		repo := filepath.Join(m.Paths.MarketplaceCache, marketplace.Name)
		stagedRepo, commit, manifest, cleanup, err := m.stageMarketplace(ctx, *marketplace)
		if err != nil {
			return updated, fmt.Errorf("update marketplace %s: %w", marketplace.Name, err)
		}
		defer cleanup()
		if manifest.Name != marketplace.Name {
			return updated, fmt.Errorf("marketplace %q now declares name %q", marketplace.Name, manifest.Name)
		}
		if err := replaceDirectory(repo, stagedRepo); err != nil {
			return updated, fmt.Errorf("activate marketplace %s: %w", marketplace.Name, err)
		}
		marketplace.Commit = commit
		marketplace.Description = manifest.Description
		updated = append(updated, *marketplace)
	}
	if !found {
		return nil, fmt.Errorf("unknown marketplace %q", name)
	}
	if err := saveRegistry(m.Paths, registry); err != nil {
		return updated, err
	}
	return updated, nil
}

func (m *Manager) RemoveMarketplace(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid marketplace name %q", name)
	}
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return err
	}
	before := len(registry.Marketplaces)
	registry.Marketplaces = slices.DeleteFunc(registry.Marketplaces, func(marketplace Marketplace) bool { return marketplace.Name == name })
	if len(registry.Marketplaces) == before {
		return fmt.Errorf("unknown marketplace %q", name)
	}
	if err := saveRegistry(m.Paths, registry); err != nil {
		return err
	}
	path := filepath.Join(m.Paths.MarketplaceCache, name)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove marketplace cache: %w", err)
	}
	return nil
}

func (m *Manager) AddPlugin(ctx context.Context, boardDir, id string) (LockedPlugin, error) {
	return m.addPlugin(ctx, boardDir, id, true)
}

func (m *Manager) addPlugin(ctx context.Context, boardDir, id string, refreshMarketplace bool) (LockedPlugin, error) {
	marketName, pluginName, ok := splitID(id)
	if !ok {
		return LockedPlugin{}, fmt.Errorf("plugin must be qualified as marketplace/plugin")
	}
	if refreshMarketplace {
		if _, err := m.UpdateMarketplaces(ctx, marketName); err != nil {
			return LockedPlugin{}, err
		}
	}
	marketplace, repo, err := m.marketplace(marketName)
	if err != nil {
		return LockedPlugin{}, err
	}
	manifest, err := LoadMarketplace(repo)
	if err != nil {
		return LockedPlugin{}, err
	}
	entry, ok := marketplaceEntry(manifest, pluginName)
	if !ok {
		return LockedPlugin{}, fmt.Errorf("marketplace %q has no plugin %q", marketName, pluginName)
	}
	source, _ := safeRelativePath(repo, entry.Source)
	pluginManifest, err := LoadPlugin(source)
	if err != nil {
		return LockedPlugin{}, err
	}
	digest, err := contentDigest(source)
	if err != nil {
		return LockedPlugin{}, fmt.Errorf("hash plugin %s: %w", id, err)
	}
	locked := LockedPlugin{
		ID: id, Version: pluginManifest.Version, Description: pluginManifest.Description,
		Marketplace: marketName, MarketplaceURL: marketplace.URL, MarketplaceRef: marketplace.Ref,
		MarketplaceCommit: marketplace.Commit, Source: entry.Source,
		Entrypoint: pluginManifest.Entrypoint, ContentSHA256: digest,
	}
	if err := m.cacheFromSource(locked, source); err != nil {
		return LockedPlugin{}, err
	}
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return LockedPlugin{}, err
	}
	replaced := false
	for i := range lock.Plugins {
		if lock.Plugins[i].ID == id {
			locked.Disabled = lock.Plugins[i].Disabled
			lock.Plugins[i] = locked
			replaced = true
			break
		}
	}
	if !replaced {
		lock.Plugins = append(lock.Plugins, locked)
	}
	if err := SaveBoardLock(boardDir, lock); err != nil {
		return LockedPlugin{}, err
	}
	return locked, nil
}

// UpdatePlugin refreshes a locked plugin. If its marketplace is not registered
// on this machine, the lock's canonical URL and ref restore that discovery
// state before the plugin is resolved.
func (m *Manager) UpdatePlugin(ctx context.Context, boardDir, id string) (LockedPlugin, error) {
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return LockedPlugin{}, err
	}
	var current LockedPlugin
	found := false
	for _, locked := range lock.Plugins {
		if locked.ID == id {
			current = locked
			found = true
			break
		}
	}
	if !found {
		return LockedPlugin{}, fmt.Errorf("plugin %q is not in this board's lock", id)
	}

	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return LockedPlugin{}, err
	}
	registered := slices.ContainsFunc(registry.Marketplaces, func(marketplace Marketplace) bool {
		return marketplace.Name == current.Marketplace
	})
	if registered {
		return m.addPlugin(ctx, boardDir, id, true)
	}
	if _, err := m.addMarketplace(ctx, current.MarketplaceURL, current.MarketplaceRef, current.Marketplace); err != nil {
		return LockedPlugin{}, fmt.Errorf("restore marketplace %s from plugin lock: %w", current.Marketplace, err)
	}
	return m.addPlugin(ctx, boardDir, id, false)
}

// UpdatePlugins resolves and verifies every requested update before replacing
// their entries with a single atomic board lock write. Candidate content may be
// cached before the write; that cache is disposable and does not activate it.
func (m *Manager) UpdatePlugins(ctx context.Context, boardDir string, ids []string) ([]LockedPlugin, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	previews, err := m.PreviewUpdates(ctx, boardDir, ids)
	if err != nil {
		return nil, err
	}

	updated := make([]LockedPlugin, len(previews))
	for i, preview := range previews {
		if err := m.fetchLocked(ctx, preview.Candidate); err != nil {
			return nil, fmt.Errorf("cache update for %s: %w", preview.ID, err)
		}
		updated[i] = preview.Candidate
	}

	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return nil, err
	}
	indexes := make([]int, len(previews))
	seen := make(map[string]bool, len(previews))
	for i, preview := range previews {
		if seen[preview.ID] {
			return nil, fmt.Errorf("plugin %q was requested more than once", preview.ID)
		}
		seen[preview.ID] = true
		index := slices.IndexFunc(lock.Plugins, func(locked LockedPlugin) bool { return locked.ID == preview.ID })
		if index < 0 || lock.Plugins[index] != preview.Current {
			return nil, fmt.Errorf("plugin %q changed in the board lock while updates were resolving", preview.ID)
		}
		indexes[i] = index
	}
	for i, index := range indexes {
		lock.Plugins[index] = updated[i]
	}
	if err := SaveBoardLock(boardDir, lock); err != nil {
		return nil, err
	}
	return updated, nil
}

func (m *Manager) RemovePlugin(boardDir, id string) error {
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return err
	}
	before := len(lock.Plugins)
	lock.Plugins = slices.DeleteFunc(lock.Plugins, func(locked LockedPlugin) bool { return locked.ID == id })
	if len(lock.Plugins) == before {
		return fmt.Errorf("plugin %q is not in this board's lock", id)
	}
	return SaveBoardLock(boardDir, lock)
}

// SetPluginEnabled changes whether a locked plugin is loaded without changing
// its pinned revision or removing it from the board lock.
func (m *Manager) SetPluginEnabled(boardDir, id string, enabled bool) error {
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(lock.Plugins, func(locked LockedPlugin) bool { return locked.ID == id })
	if index < 0 {
		return fmt.Errorf("plugin %q is not in this board's lock", id)
	}
	lock.Plugins[index].Disabled = !enabled
	return SaveBoardLock(boardDir, lock)
}

func (m *Manager) Sync(ctx context.Context, boardDir string) ([]RuntimePlugin, error) {
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return nil, err
	}
	for _, locked := range lock.Plugins {
		if _, err := m.runtimePlugin(locked); err == nil {
			continue
		}
		if err := m.fetchLocked(ctx, locked); err != nil {
			return nil, err
		}
	}
	return m.RuntimePlugins(boardDir)
}

func (m *Manager) RuntimePlugins(boardDir string) ([]RuntimePlugin, error) {
	if boardDir == "" {
		return nil, nil
	}
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return nil, err
	}
	plugins := make([]RuntimePlugin, 0, len(lock.Plugins))
	for _, locked := range lock.Plugins {
		if locked.Disabled {
			continue
		}
		runtime, err := m.runtimePlugin(locked)
		if err != nil {
			return nil, fmt.Errorf("plugin %s is not synchronized: %w; run `kbrd plugin sync`", locked.ID, err)
		}
		plugins = append(plugins, runtime)
	}
	return plugins, nil
}

func RuntimePlugins(boardDir string) ([]RuntimePlugin, error) {
	manager, err := DefaultManager()
	if err != nil {
		return nil, err
	}
	return manager.RuntimePlugins(boardDir)
}

func (m *Manager) marketplace(name string) (Marketplace, string, error) {
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return Marketplace{}, "", err
	}
	for _, marketplace := range registry.Marketplaces {
		if marketplace.Name == name {
			return marketplace, filepath.Join(m.Paths.MarketplaceCache, name), nil
		}
	}
	return Marketplace{}, "", fmt.Errorf("unknown marketplace %q; add it with `kbrd plugin marketplace add <git-url>`", name)
}

func (m *Manager) checkoutLatest(ctx context.Context, repo, ref string) (string, error) {
	commit, err := kbrdfs.GitFetchRefContext(ctx, repo, ref)
	if err != nil {
		return "", err
	}
	if err := kbrdfs.GitCheckoutDetachedContext(ctx, repo, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func (m *Manager) fetchLocked(ctx context.Context, locked LockedPlugin) error {
	if err := os.MkdirAll(m.Paths.CacheRoot, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(m.Paths.CacheRoot, ".sync-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	repo := filepath.Join(tmp, "repo")
	if err := kbrdfs.GitCloneContext(ctx, locked.MarketplaceURL, repo); err != nil {
		return fmt.Errorf("clone marketplace for %s: %w", locked.ID, err)
	}
	commit, err := kbrdfs.GitResolveRevision(repo, locked.MarketplaceCommit)
	if err != nil {
		return fmt.Errorf("resolve locked commit for %s: %w", locked.ID, err)
	}
	if commit != locked.MarketplaceCommit {
		return fmt.Errorf("resolved commit for %s changed from %s to %s", locked.ID, locked.MarketplaceCommit, commit)
	}
	if err := kbrdfs.GitCheckoutDetachedContext(ctx, repo, commit); err != nil {
		return fmt.Errorf("checkout locked commit for %s: %w", locked.ID, err)
	}
	source, err := safeRelativePath(repo, locked.Source)
	if err != nil {
		return err
	}
	manifest, err := LoadPlugin(source)
	if err != nil {
		return err
	}
	_, name, _ := splitID(locked.ID)
	if manifest.Name != name || manifest.Entrypoint != locked.Entrypoint {
		return fmt.Errorf("plugin %s manifest does not match its lock", locked.ID)
	}
	digest, err := contentDigest(source)
	if err != nil {
		return err
	}
	if digest != locked.ContentSHA256 {
		return fmt.Errorf("plugin %s content digest mismatch: got %s, want %s", locked.ID, digest, locked.ContentSHA256)
	}
	return m.cacheFromSource(locked, source)
}

func (m *Manager) cacheFromSource(locked LockedPlugin, source string) error {
	hexDigest, err := digestHex(locked.ContentSHA256)
	if err != nil {
		return err
	}
	_, pluginName, _ := splitID(locked.ID)
	cacheName := locked.Marketplace + "--" + pluginName + "--" + hexDigest
	destination := filepath.Join(m.Paths.ContentCache, cacheName)
	if _, err := m.runtimePlugin(locked); err == nil {
		return nil
	}
	if err := os.MkdirAll(m.Paths.ContentCache, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(m.Paths.ContentCache, ".content-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	target := filepath.Join(tmp, locked.Marketplace, strings.TrimPrefix(locked.ID, locked.Marketplace+"/"))
	if err := copyPluginTree(source, target); err != nil {
		return err
	}
	copiedDigest, err := contentDigest(target)
	if err != nil {
		return fmt.Errorf("verify copied plugin cache: %w", err)
	}
	if copiedDigest != locked.ContentSHA256 {
		return fmt.Errorf("copied plugin %s digest mismatch: got %s, want %s", locked.ID, copiedDigest, locked.ContentSHA256)
	}
	if err := replaceDirectory(destination, tmp); err != nil {
		return fmt.Errorf("activate plugin cache: %w", err)
	}
	return nil
}

func (m *Manager) runtimePlugin(locked LockedPlugin) (RuntimePlugin, error) {
	hexDigest, err := digestHex(locked.ContentSHA256)
	if err != nil {
		return RuntimePlugin{}, err
	}
	_, name, _ := splitID(locked.ID)
	cacheName := locked.Marketplace + "--" + name + "--" + hexDigest
	moduleRoot := filepath.Join(m.Paths.ContentCache, cacheName)
	root := filepath.Join(moduleRoot, locked.Marketplace, name)
	digest, err := contentDigest(root)
	if err != nil {
		return RuntimePlugin{}, err
	}
	if digest != locked.ContentSHA256 {
		return RuntimePlugin{}, fmt.Errorf("cached content digest mismatch")
	}
	entrypoint, err := safeRelativePath(root, locked.Entrypoint)
	if err != nil {
		return RuntimePlugin{}, err
	}
	return RuntimePlugin{ID: locked.ID, Root: root, ModuleRoot: moduleRoot, Entrypoint: entrypoint}, nil
}

func normalizeGitURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("marketplace Git URL is required")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("invalid marketplace Git URL: %w", err)
		}
		if parsed.User != nil {
			return "", fmt.Errorf("marketplace URLs must not contain credentials; configure a Git credential helper instead")
		}
		return raw, nil
	}
	if info, err := os.Stat(raw); err == nil && info.IsDir() {
		absolute, err := filepath.Abs(raw)
		if err != nil {
			return "", fmt.Errorf("resolve local marketplace path: %w", err)
		}
		return absolute, nil
	}
	return raw, nil
}

func (m *Manager) stageMarketplace(ctx context.Context, marketplace Marketplace) (string, string, MarketplaceManifest, func(), error) {
	if err := os.MkdirAll(m.Paths.MarketplaceCache, 0o700); err != nil {
		return "", "", MarketplaceManifest{}, func() {}, err
	}
	tmp, err := os.MkdirTemp(m.Paths.MarketplaceCache, ".update-*")
	if err != nil {
		return "", "", MarketplaceManifest{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	repo := filepath.Join(tmp, "repo")
	if err := kbrdfs.GitCloneContext(ctx, marketplace.URL, repo); err != nil {
		cleanup()
		return "", "", MarketplaceManifest{}, func() {}, err
	}
	commit, err := m.checkoutLatest(ctx, repo, marketplace.Ref)
	if err != nil {
		cleanup()
		return "", "", MarketplaceManifest{}, func() {}, err
	}
	manifest, err := LoadMarketplace(repo)
	if err != nil {
		cleanup()
		return "", "", MarketplaceManifest{}, func() {}, err
	}
	return repo, commit, manifest, cleanup, nil
}

func replaceDirectory(destination, staged string) error {
	backup := destination + ".previous"
	_ = os.RemoveAll(backup)
	destinationExists := false
	if _, err := os.Stat(destination); err == nil {
		destinationExists = true
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staged, destination); err != nil {
		if destinationExists {
			_ = os.Rename(backup, destination)
		}
		return err
	}
	if destinationExists {
		_ = os.RemoveAll(backup)
	}
	return nil
}
