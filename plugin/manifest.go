package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
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
		if err := validatePublishedVersions(entry); err != nil {
			return fmt.Errorf("%s: plugin %q: %w", MarketplaceFile, entry.Name, err)
		}
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

func validatePublishedVersions(entry MarketplaceEntry) error {
	versions := make(map[string]bool, len(entry.Versions))
	for _, release := range entry.Versions {
		version, err := canonicalVersion(release.Version)
		if err != nil {
			return err
		}
		if versions[version] {
			return fmt.Errorf("duplicate published version %q", release.Version)
		}
		versions[version] = true
		if err := validateGitRef(release.Ref); err != nil {
			return fmt.Errorf("version %s: %w", release.Version, err)
		}
		if release.Source != "" {
			if _, err := safeRelativePath(".", release.Source); err != nil {
				return fmt.Errorf("version %s source: %w", release.Version, err)
			}
		}
	}
	channels := make([]string, 0, len(entry.Channels))
	for channel := range entry.Channels {
		channels = append(channels, channel)
	}
	slices.Sort(channels)
	for _, channel := range channels {
		if !namePattern.MatchString(channel) {
			return fmt.Errorf("channel %q must be kebab-case", channel)
		}
		target, err := canonicalVersion(entry.Channels[channel])
		if err != nil {
			return fmt.Errorf("channel %q: %w", channel, err)
		}
		if !versions[target] {
			return fmt.Errorf("channel %q targets unpublished version %q", channel, entry.Channels[channel])
		}
	}
	return nil
}

func canonicalVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("version is required")
	}
	prefixed := version
	if !strings.HasPrefix(prefixed, "v") {
		prefixed = "v" + prefixed
	}
	if !semver.IsValid(prefixed) {
		return "", fmt.Errorf("version %q is not valid semantic versioning", version)
	}
	return strings.TrimPrefix(semver.Canonical(prefixed), "v"), nil
}

func validateGitRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("Git ref is required")
	}
	if strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, " ~^:?*[\\\t\r\n") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, "//") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.HasSuffix(ref, ".lock") {
		return fmt.Errorf("Git ref %q is invalid", ref)
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
	declarationGroups := []struct {
		name   string
		values []string
	}{
		{name: "commands", values: manifest.Commands},
		{name: "hooks", values: manifest.Hooks},
		{name: "layers", values: manifest.Layers},
		{name: "timers", values: manifest.Timers},
	}
	for _, group := range declarationGroups {
		if err := validateDeclarations(group.values); err != nil {
			return manifest, fmt.Errorf("%s: %s: %w", path, group.name, err)
		}
	}
	documentFiles := []struct {
		name string
		rel  string
	}{
		{name: "readme", rel: manifest.README},
		{name: "changelog", rel: manifest.Changelog},
	}
	for _, document := range documentFiles {
		if document.rel == "" {
			continue
		}
		file, err := safeRelativePath(root, document.rel)
		if err != nil {
			return manifest, fmt.Errorf("%s: %s: %w", path, document.name, err)
		}
		info, err := os.Stat(file)
		if err != nil {
			return manifest, fmt.Errorf("%s: %s: %w", path, document.name, err)
		}
		if !info.Mode().IsRegular() {
			return manifest, fmt.Errorf("%s: %s must be a regular file", path, document.name)
		}
	}
	return manifest, nil
}

func validateDeclarations(values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("declarations must not be empty")
		}
		if seen[value] {
			return fmt.Errorf("duplicate declaration %q", value)
		}
		seen[value] = true
	}
	return nil
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
