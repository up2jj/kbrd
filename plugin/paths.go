package plugin

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultAppDir = "kbrd"

type Paths struct {
	ConfigRoot       string
	CacheRoot        string
	RegistryFile     string
	MarketplaceCache string
	ContentCache     string
}

func DefaultPaths() (Paths, error) {
	configRoot := os.Getenv("KBRD_PLUGIN_CONFIG_DIR")
	if configRoot == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("locate user config directory: %w", err)
		}
		configRoot = filepath.Join(base, defaultAppDir, "plugins")
	}
	cacheRoot := os.Getenv("KBRD_PLUGIN_CACHE_DIR")
	if cacheRoot == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return Paths{}, fmt.Errorf("locate user cache directory: %w", err)
		}
		cacheRoot = filepath.Join(base, defaultAppDir, "plugins")
	}
	return Paths{
		ConfigRoot:       configRoot,
		CacheRoot:        cacheRoot,
		RegistryFile:     filepath.Join(configRoot, "marketplaces.json"),
		MarketplaceCache: filepath.Join(cacheRoot, "marketplaces"),
		ContentCache:     filepath.Join(cacheRoot, "content"),
	}, nil
}
