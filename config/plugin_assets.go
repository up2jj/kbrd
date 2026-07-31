package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/viper"

	"kbrd/plugin"
)

// LoadPluginFrontmatterPresets reads TOML preset files from verified plugin
// assets. IDs are namespaced so unrelated plugins cannot shadow each other.
func LoadPluginFrontmatterPresets(sources []plugin.AssetSource) ([]FrontmatterPreset, error) {
	var presets []FrontmatterPreset
	for _, source := range sources {
		loaded, err := loadPluginPresetSource(source)
		if err != nil {
			return nil, err
		}
		presets = append(presets, loaded...)
	}
	if err := validateFrontmatterPresets(presets); err != nil {
		return nil, fmt.Errorf("plugin frontmatter presets: %w", err)
	}
	return presets, nil
}

func loadPluginPresetSource(source plugin.AssetSource) ([]FrontmatterPreset, error) {
	files, err := pluginAssetFiles(source.Path, ".toml")
	if err != nil {
		return nil, fmt.Errorf("plugin %s frontmatter presets: %w", source.ID, err)
	}
	var presets []FrontmatterPreset
	for _, path := range files {
		loaded, err := loadPluginPresetFile(source.ID, path)
		if err != nil {
			return nil, err
		}
		presets = append(presets, loaded...)
	}
	return presets, nil
}

func loadPluginPresetFile(pluginID, path string) ([]FrontmatterPreset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plugin %s frontmatter presets: read %s: %w", pluginID, path, err)
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("plugin %s frontmatter presets: read %s: %w", pluginID, path, err)
	}
	presets, err := loadFrontmatterPresets(v, path)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", pluginID, err)
	}
	for i := range presets {
		presets[i].ID = pluginID + ":" + presets[i].ID
	}
	return presets, nil
}

// MergeFrontmatterPresets composes plugin defaults with board-local presets.
// A board can replace a plugin preset by declaring its fully-qualified ID.
func MergeFrontmatterPresets(pluginPresets, boardPresets []FrontmatterPreset) []FrontmatterPreset {
	return mergeScopedEntries(pluginPresets, boardPresets, func(p FrontmatterPreset) string { return p.ID })
}

// LoadCommandsWithPluginAssets composes global, verified plugin, and optional
// board-local commands in increasing-precedence order. Plugin command IDs are
// automatically namespaced as marketplace/plugin:id.
func LoadCommandsWithPluginAssets(folderPath string, sources []plugin.AssetSource, opts CommandLoadOptions) ([]Command, []CommandLoadWarning, error) {
	commands, warnings, err := loadGlobalCommands()
	if err != nil {
		return nil, warnings, err
	}
	for _, source := range sources {
		loaded, ws, err := loadPluginCommandSource(source)
		if err != nil {
			return nil, warnings, err
		}
		commands = append(commands, loaded...)
		warnings = append(warnings, ws...)
	}
	if opts.IncludeFolder && folderPath != "" {
		local, ws, err := readCommandsFile(filepath.Join(folderPath, FolderCommandsFile))
		if err != nil {
			return nil, warnings, err
		}
		commands = mergeScopedEntries(commands, local, func(c Command) string { return c.ID })
		warnings = append(warnings, ws...)
	}
	warnings = append(warnings, duplicateIDWarnings(commands, "custom commands", func(c Command) string { return c.ID }, func(c Command) string { return c.Name })...)
	return commands, warnings, nil
}

func loadGlobalCommands() ([]Command, []CommandLoadWarning, error) {
	globalDir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, nil
	}
	return readCommandsFile(filepath.Join(globalDir, AppDirName, GlobalCommandsFile))
}

func loadPluginCommandSource(source plugin.AssetSource) ([]Command, []CommandLoadWarning, error) {
	files, err := pluginAssetFiles(source.Path, ".yml", ".yaml")
	if err != nil {
		return nil, nil, fmt.Errorf("plugin %s custom commands: %w", source.ID, err)
	}
	var commands []Command
	var warnings []CommandLoadWarning
	for _, path := range files {
		loaded, ws, err := readCommandsFile(path)
		if err != nil {
			return nil, warnings, fmt.Errorf("plugin %s custom commands: %w", source.ID, err)
		}
		namespacePluginCommands(source.ID, loaded, ws)
		commands = append(commands, loaded...)
		warnings = append(warnings, ws...)
	}
	return commands, warnings, nil
}

func namespacePluginCommands(pluginID string, commands []Command, warnings []CommandLoadWarning) {
	for i := range commands {
		commands[i].ID = pluginID + ":" + commands[i].ID
	}
	for i := range warnings {
		warnings[i].Source = pluginID + ":" + warnings[i].Source
	}
}

func pluginAssetFiles(path string, extensions ...string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	extensionOK := func(path string) bool {
		ext := strings.ToLower(filepath.Ext(path))
		return slices.Contains(extensions, ext)
	}
	if !info.IsDir() {
		if !extensionOK(path) {
			return nil, fmt.Errorf("%s has unsupported extension", path)
		}
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && extensionOK(candidate) {
			files = append(files, candidate)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}
