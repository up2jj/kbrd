package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	kbrdfs "kbrd/fs"
)

// PreviewUpdate resolves and validates the latest plugin content, then compares
// it with the locked content. It does not write the board lock, marketplace
// registry, marketplace checkout, or executable content cache.
func (m *Manager) PreviewUpdate(ctx context.Context, boardDir, id string) (UpdatePreview, error) {
	previews, err := m.PreviewUpdates(ctx, boardDir, []string{id})
	if err != nil {
		return UpdatePreview{}, err
	}
	return previews[0], nil
}

// PreviewUpdates resolves updates for locked plugins while staging each
// distinct marketplace revision only once.
func (m *Manager) PreviewUpdates(ctx context.Context, boardDir string, ids []string) ([]UpdatePreview, error) {
	lock, err := LoadBoardLock(boardDir)
	if err != nil {
		return nil, err
	}
	lockedByID := make(map[string]LockedPlugin, len(lock.Plugins))
	for _, locked := range lock.Plugins {
		lockedByID[locked.ID] = locked
	}
	registry, err := loadRegistry(m.Paths)
	if err != nil {
		return nil, err
	}
	registered := make(map[string]Marketplace, len(registry.Marketplaces))
	for _, marketplace := range registry.Marketplaces {
		registered[marketplace.Name] = marketplace
	}

	type marketplaceKey struct {
		name string
		url  string
		ref  string
	}
	type stagedMarketplace struct {
		root     string
		commit   string
		manifest MarketplaceManifest
	}
	staged := make(map[marketplaceKey]stagedMarketplace)
	previews := make([]UpdatePreview, 0, len(ids))
	for _, id := range ids {
		current, ok := lockedByID[id]
		if !ok {
			return nil, fmt.Errorf("plugin %q is not in this board's lock", id)
		}
		marketplace := Marketplace{
			Name: current.Marketplace, URL: current.MarketplaceURL, Ref: current.MarketplaceRef,
		}
		if configured, ok := registered[current.Marketplace]; ok {
			marketplace = configured
		}
		key := marketplaceKey{name: marketplace.Name, url: marketplace.URL, ref: marketplace.Ref}
		candidate, ok := staged[key]
		if !ok {
			root, commit, manifest, cleanup, err := m.stageMarketplace(ctx, marketplace)
			if err != nil {
				return nil, fmt.Errorf("check plugin %s: %w", id, err)
			}
			defer cleanup()
			if manifest.Name != current.Marketplace {
				return nil, fmt.Errorf("marketplace %q now declares name %q", current.Marketplace, manifest.Name)
			}
			candidate = stagedMarketplace{root: root, commit: commit, manifest: manifest}
			staged[key] = candidate
		}
		preview, err := m.previewUpdate(ctx, current, marketplace, candidate.root, candidate.commit, candidate.manifest)
		if err != nil {
			return nil, err
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func (m *Manager) previewUpdate(ctx context.Context, current LockedPlugin, marketplace Marketplace, candidateRepo, commit string, marketplaceManifest MarketplaceManifest) (UpdatePreview, error) {
	_, pluginName, _ := splitID(current.ID)
	entry, ok := marketplaceEntry(marketplaceManifest, pluginName)
	if !ok {
		return UpdatePreview{}, fmt.Errorf("marketplace %q has no plugin %q", current.Marketplace, pluginName)
	}
	candidateRoot, err := safeRelativePath(candidateRepo, entry.Source)
	if err != nil {
		return UpdatePreview{}, fmt.Errorf("plugin %s source: %w", current.ID, err)
	}
	candidateManifest, err := LoadPlugin(candidateRoot)
	if err != nil {
		return UpdatePreview{}, fmt.Errorf("load plugin %s: %w", current.ID, err)
	}
	candidateDigest, err := contentDigest(candidateRoot)
	if err != nil {
		return UpdatePreview{}, fmt.Errorf("hash plugin %s: %w", current.ID, err)
	}
	candidate := LockedPlugin{
		ID: current.ID, Version: candidateManifest.Version, Description: candidateManifest.Description,
		Marketplace: current.Marketplace, MarketplaceURL: marketplace.URL, MarketplaceRef: marketplace.Ref,
		MarketplaceCommit: commit, Source: entry.Source, Entrypoint: candidateManifest.Entrypoint,
		ContentSHA256: candidateDigest,
	}

	currentRoot, cleanupCurrent, err := m.previewCurrentRoot(ctx, current)
	if err != nil {
		return UpdatePreview{}, err
	}
	defer cleanupCurrent()
	currentManifest, err := LoadPlugin(currentRoot)
	if err != nil {
		return UpdatePreview{}, fmt.Errorf("load locked plugin %s: %w", current.ID, err)
	}
	files, patch, err := comparePluginTrees(candidateRepo, currentRoot, candidateRoot)
	if err != nil {
		return UpdatePreview{}, fmt.Errorf("compare plugin %s: %w", current.ID, err)
	}
	return UpdatePreview{
		ID: current.ID, Current: current, Candidate: candidate,
		ManifestChanges: compareManifests(currentManifest, candidateManifest),
		Files:           files, Patch: patch,
	}, nil
}

func (m *Manager) previewCurrentRoot(ctx context.Context, locked LockedPlugin) (string, func(), error) {
	if runtime, err := m.runtimePlugin(locked); err == nil {
		return runtime.Root, func() {}, nil
	}
	if err := os.MkdirAll(m.Paths.CacheRoot, 0o700); err != nil {
		return "", func() {}, err
	}
	tmp, err := os.MkdirTemp(m.Paths.CacheRoot, ".preview-locked-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	repo := filepath.Join(tmp, "repo")
	if err := kbrdfs.GitCloneContext(ctx, locked.MarketplaceURL, repo); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("clone locked marketplace for %s: %w", locked.ID, err)
	}
	commit, err := kbrdfs.GitResolveRevision(repo, locked.MarketplaceCommit)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("resolve locked commit for %s: %w", locked.ID, err)
	}
	if commit != locked.MarketplaceCommit {
		cleanup()
		return "", func() {}, fmt.Errorf("resolved commit for %s changed from %s to %s", locked.ID, locked.MarketplaceCommit, commit)
	}
	if err := kbrdfs.GitCheckoutDetachedContext(ctx, repo, commit); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("checkout locked commit for %s: %w", locked.ID, err)
	}
	root, err := safeRelativePath(repo, locked.Source)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	digest, err := contentDigest(root)
	if err != nil || digest != locked.ContentSHA256 {
		cleanup()
		if err != nil {
			return "", func() {}, err
		}
		return "", func() {}, fmt.Errorf("plugin %s content digest mismatch: got %s, want %s", locked.ID, digest, locked.ContentSHA256)
	}
	return root, cleanup, nil
}

func compareManifests(before, after PluginManifest) []ManifestChange {
	fields := []struct {
		name string
		old  any
		new  any
	}{
		{"apiVersion", before.APIVersion, after.APIVersion},
		{"version", before.Version, after.Version},
		{"description", before.Description, after.Description},
		{"entrypoint", before.Entrypoint, after.Entrypoint},
		{"author", before.Author, after.Author},
		{"license", before.License, after.License},
		{"homepage", before.Homepage, after.Homepage},
		{"commands", before.Commands, after.Commands},
		{"hooks", before.Hooks, after.Hooks},
		{"layers", before.Layers, after.Layers},
		{"timers", before.Timers, after.Timers},
		{"networkAccess", before.NetworkAccess, after.NetworkAccess},
		{"shellAccess", before.ShellAccess, after.ShellAccess},
		{"readme", before.README, after.README},
		{"changelog", before.Changelog, after.Changelog},
	}
	var changes []ManifestChange
	for _, field := range fields {
		oldValue := jsonValue(field.old)
		newValue := jsonValue(field.new)
		if oldValue != newValue {
			changes = append(changes, ManifestChange{Field: field.name, Before: oldValue, After: newValue})
		}
	}
	return changes
}

func jsonValue(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func comparePluginTrees(repoRoot, beforeRoot, afterRoot string) ([]PluginFileChange, string, error) {
	before, err := pluginFiles(beforeRoot)
	if err != nil {
		return nil, "", err
	}
	after, err := pluginFiles(afterRoot)
	if err != nil {
		return nil, "", err
	}
	paths := make([]string, 0, len(before)+len(after))
	for path := range before {
		paths = append(paths, path)
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	var changes []PluginFileChange
	var patches []string
	for _, path := range paths {
		oldPath, oldOK := before[path]
		newPath, newOK := after[path]
		status := "modified"
		switch {
		case !oldOK:
			status, oldPath = "added", "/dev/null"
		case !newOK:
			status, newPath = "removed", "/dev/null"
		default:
			oldData, err := os.ReadFile(oldPath)
			if err != nil {
				return nil, "", err
			}
			newData, err := os.ReadFile(newPath)
			if err != nil {
				return nil, "", err
			}
			if string(oldData) == string(newData) {
				continue
			}
		}
		patch, err := kbrdfs.GitDiffFiles(repoRoot, oldPath, newPath)
		if err != nil {
			return nil, "", err
		}
		patches = append(patches, normalizePatchPaths(patch, oldPath, newPath, path))
		changes = append(changes, PluginFileChange{Path: path, Status: status})
	}
	return changes, strings.Join(patches, "\n"), nil
}

func pluginFiles(root string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = path
		return nil
	})
	return files, err
}

func normalizePatchPaths(patch, before, after, path string) string {
	lines := strings.Split(patch, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "diff --git ") && !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
			continue
		}
		for _, source := range []string{before, after} {
			if source == "/dev/null" {
				continue
			}
			line = strings.ReplaceAll(line, "a"+source, "a/"+path)
			line = strings.ReplaceAll(line, "b"+source, "b/"+path)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
