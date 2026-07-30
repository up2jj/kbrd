package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	kbrdfs "kbrd/fs"

	"golang.org/x/mod/semver"
)

type versionSelection struct {
	version string
	channel string
}

func parsePluginRequest(request string) (string, versionSelection, error) {
	id, version, hasVersion := strings.Cut(request, "@")
	if strings.Contains(version, "@") {
		return "", versionSelection{}, fmt.Errorf("plugin version request %q is invalid", request)
	}
	if _, _, ok := splitID(id); !ok {
		return "", versionSelection{}, fmt.Errorf("plugin must be qualified as marketplace/plugin")
	}
	if !hasVersion {
		return id, versionSelection{}, nil
	}
	canonical, err := canonicalVersion(version)
	if err != nil {
		return "", versionSelection{}, err
	}
	return id, versionSelection{version: canonical}, nil
}

func updateSelection(current LockedPlugin, options []UpdateOptions) (versionSelection, error) {
	if len(options) > 1 {
		return versionSelection{}, fmt.Errorf("only one update options value is supported")
	}
	if len(options) == 1 && options[0].Channel != "" {
		channel := strings.TrimSpace(options[0].Channel)
		if !namePattern.MatchString(channel) {
			return versionSelection{}, fmt.Errorf("channel %q must be kebab-case", options[0].Channel)
		}
		return versionSelection{channel: channel}, nil
	}
	if current.RequestedVersion != "" {
		return versionSelection{version: current.RequestedVersion}, nil
	}
	if current.Channel != "" {
		return versionSelection{channel: current.Channel}, nil
	}
	return versionSelection{}, nil
}

func selectPublishedVersion(entry MarketplaceEntry, selection versionSelection) (*PluginVersion, versionSelection, error) {
	if len(entry.Versions) == 0 {
		if selection.channel != "" {
			return nil, selection, fmt.Errorf("plugin %q does not publish version channels", entry.Name)
		}
		return nil, selection, nil
	}
	if selection.version != "" {
		release, ok := publishedVersion(entry, selection.version)
		if !ok {
			return nil, selection, fmt.Errorf("plugin %q has no published version %s", entry.Name, selection.version)
		}
		return &release, selection, nil
	}
	channel := selection.channel
	if channel == "" {
		channel = "stable"
		selection.channel = channel
	}
	if target, ok := entry.Channels[channel]; ok {
		release, _ := publishedVersion(entry, target)
		return &release, selection, nil
	}
	if channel != "stable" {
		return nil, selection, fmt.Errorf("plugin %q has no %q channel", entry.Name, channel)
	}
	stable := slices.Collect(slices.Values(entry.Versions))
	slices.SortFunc(stable, func(a, b PluginVersion) int {
		av, _ := canonicalVersion(a.Version)
		bv, _ := canonicalVersion(b.Version)
		return semver.Compare("v"+bv, "v"+av)
	})
	for _, release := range stable {
		version, _ := canonicalVersion(release.Version)
		if semver.Prerelease("v"+version) == "" {
			return &release, selection, nil
		}
	}
	return nil, selection, fmt.Errorf("plugin %q has no stable published version", entry.Name)
}

func publishedVersion(entry MarketplaceEntry, requested string) (PluginVersion, bool) {
	requested, err := canonicalVersion(requested)
	if err != nil {
		return PluginVersion{}, false
	}
	for _, release := range entry.Versions {
		version, _ := canonicalVersion(release.Version)
		if version == requested {
			return release, true
		}
	}
	return PluginVersion{}, false
}

// resolveCatalogPlugin resolves a version selector against a catalog checkout.
// The returned cleanup owns only a separately staged published revision; the
// caller remains responsible for the catalog checkout itself.
func (m *Manager) resolveCatalogPlugin(
	ctx context.Context,
	marketplace Marketplace,
	catalogRoot, catalogCommit, id string,
	selection versionSelection,
) (LockedPlugin, string, func(), error) {
	_, pluginName, _ := splitID(id)
	catalog, err := LoadMarketplace(catalogRoot)
	if err != nil {
		return LockedPlugin{}, "", func() {}, err
	}
	entry, ok := marketplaceEntry(catalog, pluginName)
	if !ok {
		return LockedPlugin{}, "", func() {}, fmt.Errorf("marketplace %q has no plugin %q", marketplace.Name, pluginName)
	}
	release, effectiveSelection, err := selectPublishedVersion(entry, selection)
	if err != nil {
		return LockedPlugin{}, "", func() {}, err
	}

	repo := catalogRoot
	commit := catalogCommit
	cleanup := func() {}
	sourcePath := entry.Source
	if release != nil {
		if release.Source != "" {
			sourcePath = release.Source
		}
		if err := os.MkdirAll(m.Paths.CacheRoot, 0o700); err != nil {
			return LockedPlugin{}, "", cleanup, err
		}
		tmp, err := os.MkdirTemp(m.Paths.CacheRoot, ".resolve-version-*")
		if err != nil {
			return LockedPlugin{}, "", cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(tmp) }
		repo = filepath.Join(tmp, "repo")
		if err := kbrdfs.GitCloneContext(ctx, marketplace.URL, repo); err != nil {
			cleanup()
			return LockedPlugin{}, "", func() {}, fmt.Errorf("clone marketplace for %s: %w", id, err)
		}
		commit, err = m.checkoutLatest(ctx, repo, release.Ref)
		if err != nil {
			cleanup()
			return LockedPlugin{}, "", func() {}, fmt.Errorf("resolve plugin %s version %s: %w", id, release.Version, err)
		}
	}

	source, err := safeRelativePath(repo, sourcePath)
	if err != nil {
		cleanup()
		return LockedPlugin{}, "", func() {}, fmt.Errorf("plugin %s source: %w", id, err)
	}
	manifest, err := LoadPlugin(source)
	if err != nil {
		cleanup()
		return LockedPlugin{}, "", func() {}, fmt.Errorf("load plugin %s: %w", id, err)
	}
	if manifest.Name != pluginName {
		cleanup()
		return LockedPlugin{}, "", func() {}, fmt.Errorf("plugin %s resolved manifest name %q", id, manifest.Name)
	}
	if release != nil || effectiveSelection.version != "" {
		expected := effectiveSelection.version
		if release != nil {
			expected, _ = canonicalVersion(release.Version)
		}
		actual, versionErr := canonicalVersion(manifest.Version)
		if versionErr != nil || actual != expected {
			cleanup()
			return LockedPlugin{}, "", func() {}, fmt.Errorf("plugin %s resolved version %q, want %s", id, manifest.Version, expected)
		}
	}
	digest, err := contentDigest(source)
	if err != nil {
		cleanup()
		return LockedPlugin{}, "", func() {}, fmt.Errorf("hash plugin %s: %w", id, err)
	}
	locked := LockedPlugin{
		ID: id, Version: manifest.Version, Description: manifest.Description,
		Marketplace: marketplace.Name, MarketplaceURL: marketplace.URL, MarketplaceRef: marketplace.Ref,
		MarketplaceCommit: commit, Source: sourcePath, Entrypoint: manifest.Entrypoint,
		ContentSHA256: digest,
	}
	if effectiveSelection.version != "" {
		locked.RequestedVersion = effectiveSelection.version
	} else if effectiveSelection.channel != "" {
		locked.Channel = effectiveSelection.channel
	}
	return locked, source, cleanup, nil
}
