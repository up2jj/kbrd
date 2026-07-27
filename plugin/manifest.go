package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const maxManifestBytes = 1 << 20

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func readJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxManifestBytes {
		return fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func LoadMarketplace(root string) (MarketplaceManifest, error) {
	var manifest MarketplaceManifest
	path := filepath.Join(root, MarketplaceFile)
	if err := readJSON(path, &manifest); err != nil {
		return manifest, fmt.Errorf("read %s: %w", path, err)
	}
	if err := validateMarketplace(root, manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateMarketplace(root string, manifest MarketplaceManifest) error {
	if manifest.APIVersion != APIVersion {
		return fmt.Errorf("%s: unsupported apiVersion %d", MarketplaceFile, manifest.APIVersion)
	}
	if !namePattern.MatchString(manifest.Name) {
		return fmt.Errorf("%s: name %q must be kebab-case", MarketplaceFile, manifest.Name)
	}
	seen := make(map[string]bool, len(manifest.Plugins))
	for _, entry := range manifest.Plugins {
		if !namePattern.MatchString(entry.Name) {
			return fmt.Errorf("%s: plugin name %q must be kebab-case", MarketplaceFile, entry.Name)
		}
		if seen[entry.Name] {
			return fmt.Errorf("%s: duplicate plugin %q", MarketplaceFile, entry.Name)
		}
		seen[entry.Name] = true
		source, err := safeRelativePath(root, entry.Source)
		if err != nil {
			return fmt.Errorf("%s: plugin %q source: %w", MarketplaceFile, entry.Name, err)
		}
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("%s: plugin %q source: %w", MarketplaceFile, entry.Name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s: plugin %q source is not a directory", MarketplaceFile, entry.Name)
		}
		pluginManifest, err := LoadPlugin(source)
		if err != nil {
			return fmt.Errorf("%s: plugin %q: %w", MarketplaceFile, entry.Name, err)
		}
		if pluginManifest.Name != entry.Name {
			return fmt.Errorf("%s: catalog name %q does not match plugin.json name %q", MarketplaceFile, entry.Name, pluginManifest.Name)
		}
		if _, err := contentDigest(source); err != nil {
			return fmt.Errorf("%s: plugin %q: %w", MarketplaceFile, entry.Name, err)
		}
	}
	return nil
}

func LoadPlugin(root string) (PluginManifest, error) {
	var manifest PluginManifest
	path := filepath.Join(root, PluginFile)
	if err := readJSON(path, &manifest); err != nil {
		return manifest, fmt.Errorf("read %s: %w", path, err)
	}
	if manifest.APIVersion != APIVersion {
		return manifest, fmt.Errorf("%s: unsupported apiVersion %d", path, manifest.APIVersion)
	}
	if !namePattern.MatchString(manifest.Name) {
		return manifest, fmt.Errorf("%s: name %q must be kebab-case", path, manifest.Name)
	}
	if strings.TrimSpace(manifest.Description) == "" {
		return manifest, fmt.Errorf("%s: description is required", path)
	}
	if manifest.Entrypoint == "" {
		return manifest, fmt.Errorf("%s: entrypoint is required", path)
	}
	entrypoint, err := safeRelativePath(root, manifest.Entrypoint)
	if err != nil {
		return manifest, fmt.Errorf("%s: entrypoint: %w", path, err)
	}
	info, err := os.Stat(entrypoint)
	if err != nil {
		return manifest, fmt.Errorf("%s: entrypoint: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return manifest, fmt.Errorf("%s: entrypoint must be a regular file", path)
	}
	return manifest, nil
}

func safeRelativePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes its root")
	}
	full := filepath.Join(root, clean)
	relToRoot, err := filepath.Rel(root, full)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes its root")
	}
	return full, nil
}

func marketplaceEntry(manifest MarketplaceManifest, name string) (MarketplaceEntry, bool) {
	i := slices.IndexFunc(manifest.Plugins, func(entry MarketplaceEntry) bool { return entry.Name == name })
	if i < 0 {
		return MarketplaceEntry{}, false
	}
	return manifest.Plugins[i], true
}
