package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	kbrdfs "kbrd/fs"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func loadRegistry(paths Paths) (Registry, error) {
	registry := Registry{APIVersion: APIVersion}
	if err := readJSON(paths.RegistryFile, &registry); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return registry, nil
		}
		return Registry{}, fmt.Errorf("load marketplace registry: %w", err)
	}
	if registry.APIVersion != APIVersion {
		return Registry{}, fmt.Errorf("marketplace registry: unsupported apiVersion %d", registry.APIVersion)
	}
	slices.SortFunc(registry.Marketplaces, func(a, b Marketplace) int { return cmpString(a.Name, b.Name) })
	return registry, nil
}

func saveRegistry(paths Paths, registry Registry) error {
	registry.APIVersion = APIVersion
	slices.SortFunc(registry.Marketplaces, func(a, b Marketplace) int { return cmpString(a.Name, b.Name) })
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode marketplace registry: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(paths.RegistryFile), 0o700); err != nil {
		return fmt.Errorf("create plugin config directory: %w", err)
	}
	if err := kbrdfs.WriteFileAtomicDurable(paths.RegistryFile, data, 0o600); err != nil {
		return fmt.Errorf("write marketplace registry: %w", err)
	}
	return nil
}

func LoadBoardLock(boardDir string) (BoardLock, error) {
	lock := BoardLock{APIVersion: APIVersion}
	path := filepath.Join(boardDir, LockFile)
	if err := readJSON(path, &lock); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lock, nil
		}
		return BoardLock{}, fmt.Errorf("load %s: %w", path, err)
	}
	if lock.APIVersion != APIVersion {
		return BoardLock{}, fmt.Errorf("%s: unsupported apiVersion %d", path, lock.APIVersion)
	}
	seen := make(map[string]bool, len(lock.Plugins))
	for _, locked := range lock.Plugins {
		if err := validateLockedPlugin(locked); err != nil {
			return BoardLock{}, fmt.Errorf("%s: %w", path, err)
		}
		if seen[locked.ID] {
			return BoardLock{}, fmt.Errorf("%s: duplicate plugin %q", path, locked.ID)
		}
		seen[locked.ID] = true
	}
	for _, historical := range lock.History {
		if err := validateLockedPlugin(historical); err != nil {
			return BoardLock{}, fmt.Errorf("%s history: %w", path, err)
		}
	}
	slices.SortFunc(lock.Plugins, func(a, b LockedPlugin) int { return cmpString(a.ID, b.ID) })
	return lock, nil
}

func SaveBoardLock(boardDir string, lock BoardLock) error {
	lock.APIVersion = APIVersion
	slices.SortFunc(lock.Plugins, func(a, b LockedPlugin) int { return cmpString(a.ID, b.ID) })
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin lock: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(boardDir, LockFile)
	if err := kbrdfs.WriteFileAtomicDurable(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func validateLockedPlugin(locked LockedPlugin) error {
	market, name, ok := splitID(locked.ID)
	if !ok || market != locked.Marketplace {
		return fmt.Errorf("plugin id %q is invalid or does not match marketplace %q", locked.ID, locked.Marketplace)
	}
	if !namePattern.MatchString(name) || locked.MarketplaceURL == "" || !commitPattern.MatchString(locked.MarketplaceCommit) || locked.ContentSHA256 == "" {
		return fmt.Errorf("plugin %q has incomplete lock metadata", locked.ID)
	}
	if strings.Contains(locked.MarketplaceURL, "://") {
		parsed, err := url.Parse(locked.MarketplaceURL)
		if err != nil || parsed.User != nil {
			return fmt.Errorf("plugin %q has an invalid or credential-bearing marketplace URL", locked.ID)
		}
	}
	if _, err := digestHex(locked.ContentSHA256); err != nil {
		return fmt.Errorf("plugin %q: %w", locked.ID, err)
	}
	if _, err := safeRelativePath(".", locked.Source); err != nil {
		return fmt.Errorf("plugin %q source: %w", locked.ID, err)
	}
	if _, err := safeRelativePath(".", locked.Entrypoint); err != nil {
		return fmt.Errorf("plugin %q entrypoint: %w", locked.ID, err)
	}
	if locked.RequestedVersion != "" {
		if _, err := canonicalVersion(locked.RequestedVersion); err != nil {
			return fmt.Errorf("plugin %q requested version: %w", locked.ID, err)
		}
	}
	if locked.Channel != "" && !namePattern.MatchString(locked.Channel) {
		return fmt.Errorf("plugin %q has invalid channel %q", locked.ID, locked.Channel)
	}
	if locked.RequestedVersion != "" && locked.Channel != "" {
		return fmt.Errorf("plugin %q lock selects both a version and a channel", locked.ID)
	}
	return nil
}

func splitID(id string) (string, string, bool) {
	for i := 0; i < len(id); i++ {
		if id[i] != '/' {
			continue
		}
		market, name := id[:i], id[i+1:]
		return market, name, namePattern.MatchString(market) && namePattern.MatchString(name)
	}
	return "", "", false
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
