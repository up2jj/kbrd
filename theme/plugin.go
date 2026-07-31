package theme

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"kbrd/plugin"
)

var pluginThemeNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type pluginThemeDefinition struct {
	Name    string               `toml:"name"`
	Base    string               `toml:"base"`
	Palette pluginPaletteOverlay `toml:"palette"`
}

// pluginPaletteOverlay uses pointers to distinguish an omitted color from an
// explicitly empty one. Strict TOML decoding catches misspelled field names.
type pluginPaletteOverlay struct {
	FgStrong          *string `toml:"fg_strong"`
	FgEmphasis        *string `toml:"fg_emphasis"`
	FgBase            *string `toml:"fg_base"`
	FgSoft            *string `toml:"fg_soft"`
	FgMuted           *string `toml:"fg_muted"`
	FgSubtle          *string `toml:"fg_subtle"`
	FgDim             *string `toml:"fg_dim"`
	FgInverse         *string `toml:"fg_inverse"`
	FgOnAccent        *string `toml:"fg_on_accent"`
	BorderActive      *string `toml:"border_active"`
	BorderMuted       *string `toml:"border_muted"`
	Primary           *string `toml:"primary"`
	PrimaryStrong     *string `toml:"primary_strong"`
	AccentSoft        *string `toml:"accent_soft"`
	FgSelectedPreview *string `toml:"fg_selected_preview"`
	BgSelectedDetail  *string `toml:"bg_selected_detail"`
	Link              *string `toml:"link"`
	AccentAlt         *string `toml:"accent_alt"`
	Success           *string `toml:"success"`
	Danger            *string `toml:"danger"`
	Warning           *string `toml:"warning"`
	WarningSoft       *string `toml:"warning_soft"`
	Highlight         *string `toml:"highlight"`
	BgCodeInline      *string `toml:"bg_code_inline"`
	BgCodeBlock       *string `toml:"bg_code_block"`
	FgCodeBlock       *string `toml:"fg_code_block"`
}

type loadedPluginTheme struct {
	id      string
	palette Palette
}

// LoadPluginThemes loads namespaced, read-only themes from verified plugin
// assets. Theme files are TOML overlays on the built-in dark or light palette.
func LoadPluginThemes(sources []plugin.AssetSource, dark, light Palette) (map[string]Palette, error) {
	themes := make(map[string]Palette)
	for _, source := range sources {
		loaded, err := loadPluginThemeSource(source, dark, light)
		if err != nil {
			return nil, err
		}
		for _, candidate := range loaded {
			if _, exists := themes[candidate.id]; exists {
				return nil, fmt.Errorf("duplicate plugin theme %q", candidate.id)
			}
			themes[candidate.id] = candidate.palette
		}
	}
	return themes, nil
}

func loadPluginThemeSource(source plugin.AssetSource, dark, light Palette) ([]loadedPluginTheme, error) {
	files, err := pluginThemeFiles(source.Path)
	if err != nil {
		return nil, fmt.Errorf("plugin %s themes: %w", source.ID, err)
	}
	themes := make([]loadedPluginTheme, 0, len(files))
	for _, path := range files {
		name, palette, err := loadPluginTheme(path, dark, light)
		if err != nil {
			return nil, fmt.Errorf("plugin %s themes: %s: %w", source.ID, path, err)
		}
		themes = append(themes, loadedPluginTheme{id: source.ID + "/" + name, palette: palette})
	}
	return themes, nil
}

func loadPluginTheme(path string, dark, light Palette) (string, Palette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", Palette{}, fmt.Errorf("read file: %w", err)
	}
	var definition pluginThemeDefinition
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return "", Palette{}, fmt.Errorf("parse TOML: %w", err)
	}
	if definition.Name == "" {
		definition.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if !pluginThemeNamePattern.MatchString(definition.Name) {
		return "", Palette{}, fmt.Errorf("name %q must be kebab-case", definition.Name)
	}
	palette, err := pluginBasePalette(definition.Base, dark, light)
	if err != nil {
		return "", Palette{}, err
	}
	if err := definition.Palette.apply(&palette); err != nil {
		return "", Palette{}, err
	}
	return definition.Name, palette, nil
}

func pluginBasePalette(base string, dark, light Palette) (Palette, error) {
	switch base {
	case "", "dark":
		return dark, nil
	case "light":
		return light, nil
	default:
		return Palette{}, fmt.Errorf("base must be dark or light")
	}
}

func pluginThemeFiles(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".toml") {
			return nil, fmt.Errorf("%s is not a TOML file", path)
		}
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(candidate), ".toml") {
			files = append(files, candidate)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

func (o pluginPaletteOverlay) apply(palette *Palette) error {
	fields := []struct {
		name   string
		value  *string
		target *Color
	}{
		{"fg_strong", o.FgStrong, &palette.FgStrong},
		{"fg_emphasis", o.FgEmphasis, &palette.FgEmphasis},
		{"fg_base", o.FgBase, &palette.FgBase},
		{"fg_soft", o.FgSoft, &palette.FgSoft},
		{"fg_muted", o.FgMuted, &palette.FgMuted},
		{"fg_subtle", o.FgSubtle, &palette.FgSubtle},
		{"fg_dim", o.FgDim, &palette.FgDim},
		{"fg_inverse", o.FgInverse, &palette.FgInverse},
		{"fg_on_accent", o.FgOnAccent, &palette.FgOnAccent},
		{"border_active", o.BorderActive, &palette.BorderActive},
		{"border_muted", o.BorderMuted, &palette.BorderMuted},
		{"primary", o.Primary, &palette.Primary},
		{"primary_strong", o.PrimaryStrong, &palette.PrimaryStrong},
		{"accent_soft", o.AccentSoft, &palette.AccentSoft},
		{"fg_selected_preview", o.FgSelectedPreview, &palette.FgSelectedPreview},
		{"bg_selected_detail", o.BgSelectedDetail, &palette.BgSelectedDetail},
		{"link", o.Link, &palette.Link},
		{"accent_alt", o.AccentAlt, &palette.AccentAlt},
		{"success", o.Success, &palette.Success},
		{"danger", o.Danger, &palette.Danger},
		{"warning", o.Warning, &palette.Warning},
		{"warning_soft", o.WarningSoft, &palette.WarningSoft},
		{"highlight", o.Highlight, &palette.Highlight},
		{"bg_code_inline", o.BgCodeInline, &palette.BgCodeInline},
		{"bg_code_block", o.BgCodeBlock, &palette.BgCodeBlock},
		{"fg_code_block", o.FgCodeBlock, &palette.FgCodeBlock},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		if strings.TrimSpace(*field.value) == "" {
			return fmt.Errorf("palette.%s must not be empty", field.name)
		}
		*field.target = Color(*field.value)
	}
	return nil
}
