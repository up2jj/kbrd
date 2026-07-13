package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/viper"
)

// FrontmatterPreset describes a board-local, declarative patch for card
// frontmatter. Set values may contain the built-in {{...}} variables; unset
// keys are always literal frontmatter keys.
type FrontmatterPreset struct {
	ID          string `mapstructure:"id"`
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
	// Columns accepts names and 1-based filesystem-column positions. Keeping
	// the raw values lets a board mix selectors while remaining independent of
	// the current directory names.
	Columns []any          `mapstructure:"columns"`
	Set     map[string]any `mapstructure:"set"`
	Unset   []string       `mapstructure:"unset"`
}

// AppliesTo reports whether the preset should be offered for a filesystem
// column. position is the 1-based filesystem-column position. An empty
// Columns list means board-wide availability.
func (p FrontmatterPreset) AppliesTo(column string, position int) bool {
	if len(p.Columns) == 0 {
		return true
	}
	for _, allowed := range p.Columns {
		if name, ok := allowed.(string); ok && name == column {
			return true
		}
		if index, ok := presetColumnIndex(allowed); ok && index == position {
			return true
		}
	}
	return false
}

var presetVariables = map[string]struct{}{
	"now": {}, "today": {}, "board": {}, "column": {}, "filename": {}, "user": {},
}

// loadFrontmatterPresets decodes only the board-local preset table. Presets
// deliberately do not participate in the global config layer: their card
// metadata vocabulary belongs with the board repository.
func loadFrontmatterPresets(v *viper.Viper, source string) ([]FrontmatterPreset, error) {
	if v == nil || v.Get("frontmatter_presets") == nil {
		return nil, nil
	}

	var presets []FrontmatterPreset
	if err := v.UnmarshalKey("frontmatter_presets", &presets); err != nil {
		return nil, fmt.Errorf("decode frontmatter_presets in %s: %w", source, err)
	}
	if err := validateFrontmatterPresets(presets); err != nil {
		return nil, fmt.Errorf("frontmatter_presets in %s: %w", source, err)
	}
	return presets, nil
}

func validateFrontmatterPresets(presets []FrontmatterPreset) error {
	seen := make(map[string]string, len(presets))
	for i, preset := range presets {
		label := fmt.Sprintf("entry %d", i)
		if preset.ID != "" {
			label = fmt.Sprintf("%q", preset.ID)
		}
		if strings.TrimSpace(preset.ID) == "" {
			return fmt.Errorf("%s: id is required", label)
		}
		if strings.TrimSpace(preset.Name) == "" {
			return fmt.Errorf("preset %q: name is required", preset.ID)
		}
		if previous, ok := seen[preset.ID]; ok {
			return fmt.Errorf("preset %q: duplicate id (already used by %s)", preset.ID, previous)
		}
		seen[preset.ID] = label

		if len(preset.Set) == 0 && len(preset.Unset) == 0 {
			return fmt.Errorf("preset %q: at least one set.* or unset entry is required", preset.ID)
		}

		unset := make(map[string]struct{}, len(preset.Unset))
		for _, key := range preset.Unset {
			if err := validatePresetKey(key); err != nil {
				return fmt.Errorf("preset %q unset key: %w", preset.ID, err)
			}
			if _, ok := unset[key]; ok {
				return fmt.Errorf("preset %q: duplicate unset key %q", preset.ID, key)
			}
			unset[key] = struct{}{}
		}

		for key, value := range preset.Set {
			if err := validatePresetKey(key); err != nil {
				return fmt.Errorf("preset %q set key: %w", preset.ID, err)
			}
			if _, ok := unset[key]; ok {
				return fmt.Errorf("preset %q: key %q appears in both set and unset", preset.ID, key)
			}
			if err := validatePresetValue(value); err != nil {
				return fmt.Errorf("preset %q set.%s: %w", preset.ID, key, err)
			}
		}

		for _, column := range preset.Columns {
			if name, ok := column.(string); ok {
				if strings.TrimSpace(name) == "" {
					return fmt.Errorf("preset %q: columns cannot contain an empty name", preset.ID)
				}
				continue
			}
			position, ok := presetColumnIndex(column)
			if !ok {
				return fmt.Errorf("preset %q: column selector %T is unsupported; use a name or positive integer", preset.ID, column)
			}
			if position < 1 {
				return fmt.Errorf("preset %q: numeric column selectors must be positive 1-based positions", preset.ID)
			}
		}
	}
	return nil
}

func presetColumnIndex(value any) (int, bool) {
	// Viper decodes into any, so accept every integer kind while rejecting
	// floats, booleans, and values that overflow int.
	n := reflect.ValueOf(value)
	if !n.IsValid() {
		return 0, false
	}

	switch n.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		original := n.Int()
		converted := int(original)
		return converted, int64(converted) == original
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		original := n.Uint()
		converted := int(original)
		return converted, converted >= 0 && uint64(converted) == original
	default:
		return 0, false
	}
}

func validatePresetKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("key %q must not have leading or trailing whitespace", key)
	}
	if strings.ContainsAny(key, "\r\n:") {
		return fmt.Errorf("key %q contains an unsupported character", key)
	}
	return nil
}

func validatePresetValue(value any) error {
	switch v := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		if s, ok := v.(string); ok {
			return validatePresetTemplate(s)
		}
		return nil
	case []any:
		for _, item := range v {
			if err := validatePresetValue(item); err != nil {
				return err
			}
		}
		return nil
	case []string:
		for _, item := range v {
			if err := validatePresetTemplate(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("value type %T is not supported; use a scalar or flat array", value)
	}
}

func validatePresetTemplate(value string) error {
	for {
		start := strings.Index(value, "{{")
		if start < 0 {
			return nil
		}
		end := strings.Index(value[start+2:], "}}")
		if end < 0 {
			return fmt.Errorf("unterminated variable in %q", value)
		}
		end += start + 2
		name := strings.TrimSpace(value[start+2 : end])
		if _, ok := presetVariables[name]; !ok {
			if _, candidate, err := ParsePresetDateExpression(name); candidate {
				if err != nil {
					return fmt.Errorf("invalid date expression {{%s}}: %w", name, err)
				}
			} else {
				return fmt.Errorf("unknown variable {{%s}}", name)
			}
		}
		value = value[end+2:]
	}
}
