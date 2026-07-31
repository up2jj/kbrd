package model

import (
	"fmt"

	"kbrd/config"
	"kbrd/plugin"
	"kbrd/template"
	"kbrd/theme"
)

// pluginAssetRuntime owns all native content derived from the current board's
// plugin lock. Keeping the state together makes board switches and recovery
// retries atomic instead of requiring several fields to be reset in concert.
type pluginAssetRuntime struct {
	packs         []plugin.RuntimeAssets
	themes        map[string]Palette
	pluginPresets []config.FrontmatterPreset
	boardPresets  []config.FrontmatterPreset
	skipLocked    bool
}

func newPluginAssetRuntime(boardPresets []config.FrontmatterPreset) pluginAssetRuntime {
	var assets pluginAssetRuntime
	assets.setBoardPresets(boardPresets)
	return assets
}

func (a *pluginAssetRuntime) reset(boardPresets []config.FrontmatterPreset) {
	*a = newPluginAssetRuntime(boardPresets)
}

func (a *pluginAssetRuntime) setBoardPresets(presets []config.FrontmatterPreset) {
	a.boardPresets = append([]config.FrontmatterPreset(nil), presets...)
}

func (a *pluginAssetRuntime) disableLocked() {
	a.skipLocked = true
}

// load resolves the lock into temporary values and publishes them only after
// every asset type validates. A failed load cannot leave a partially updated
// runtime behind.
func (a *pluginAssetRuntime) load(boardPath, selectedTheme string) error {
	if a.skipLocked {
		a.packs = nil
		a.themes = nil
		a.pluginPresets = nil
		return nil
	}

	packs, err := plugin.RuntimeAssetPacks(boardPath)
	if err != nil {
		return err
	}
	presetSources, themeSources := nativeAssetSources(packs)
	presets, err := config.LoadPluginFrontmatterPresets(presetSources)
	if err != nil {
		return err
	}
	themes, err := theme.LoadPluginThemes(themeSources, DarkPalette(), LightPalette())
	if err != nil {
		return err
	}
	if err := requireSelectedPluginTheme(selectedTheme, themes); err != nil {
		return err
	}

	a.packs = packs
	a.pluginPresets = presets
	a.themes = themes
	return nil
}

func nativeAssetSources(packs []plugin.RuntimeAssets) ([]plugin.AssetSource, []plugin.AssetSource) {
	presets := make([]plugin.AssetSource, 0, len(packs))
	themes := make([]plugin.AssetSource, 0, len(packs))
	for _, pack := range packs {
		if pack.FrontmatterPresets != "" {
			presets = append(presets, plugin.AssetSource{ID: pack.ID, Path: pack.FrontmatterPresets})
		}
		if pack.Themes != "" {
			themes = append(themes, plugin.AssetSource{ID: pack.ID, Path: pack.Themes})
		}
	}
	return presets, themes
}

func requireSelectedPluginTheme(selected string, themes map[string]Palette) error {
	if !config.IsPluginTheme(selected) {
		return nil
	}
	if _, ok := themes[selected]; !ok {
		return fmt.Errorf("configured plugin theme %q is not available", selected)
	}
	return nil
}

func (a pluginAssetRuntime) frontmatterPresets() []config.FrontmatterPreset {
	return config.MergeFrontmatterPresets(a.pluginPresets, a.boardPresets)
}

func (a pluginAssetRuntime) commandSources() []plugin.AssetSource {
	var sources []plugin.AssetSource
	for _, pack := range a.packs {
		if pack.CustomCommands != "" {
			sources = append(sources, plugin.AssetSource{ID: pack.ID, Path: pack.CustomCommands})
		}
	}
	return sources
}

func (a pluginAssetRuntime) templateSources() []plugin.AssetSource {
	var sources []plugin.AssetSource
	for _, pack := range a.packs {
		if pack.CardTemplates != "" {
			sources = append(sources, plugin.AssetSource{ID: pack.ID, Path: pack.CardTemplates})
		}
	}
	return sources
}

// loadPluginAssets runs before Lua so static-only locks get the same
// missing-cache recovery path as executable plugins.
func (b *Board) loadPluginAssets() error {
	if err := b.pluginAssets.load(b.cfg.Path, b.cfg.Theme); err != nil {
		return err
	}
	b.cfg.FrontmatterPresets = b.pluginAssets.frontmatterPresets()
	b.theme = b.cfg.Theme
	b.applyPalette()
	return nil
}

func (b *Board) pluginCommandSources() []plugin.AssetSource {
	if b.safeMode {
		return nil
	}
	return b.pluginAssets.commandSources()
}

func (b *Board) listTemplates(columnPath string) ([]template.Template, []template.LoadWarning, error) {
	return template.ListWithPluginAssets(b.cfg.Path, columnPath, b.pluginAssets.templateSources())
}
