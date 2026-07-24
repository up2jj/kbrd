package companion

import (
	"fmt"
	"slices"
	"strings"

	"kbrd/config"
)

const defaultShortcut = "command+shift+k"

// Carbon modifier masks consumed by RegisterEventHotKey.
const (
	commandModifier = 1 << 8
	shiftModifier   = 1 << 9
	optionModifier  = 1 << 11
	controlModifier = 1 << 12
)

// HotKey is the native registration data returned to the companion app.
type HotKey struct {
	KeyCode   uint32 `json:"key_code"`
	Modifiers uint32 `json:"modifiers"`
	Label     string `json:"label"`
}

var keyCodes = map[string]uint32{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7,
	"c": 8, "v": 9, "b": 11, "q": 12, "w": 13, "e": 14, "r": 15,
	"y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22,
	"5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28, "0": 29,
	"]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "return": 36,
	"l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42, ",": 43,
	"/": 44, "n": 45, "m": 46, ".": 47, "tab": 48, "space": 49,
	"`": 50, "delete": 51, "escape": 53,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

// LoadHotKey reads and validates the global companion shortcut.
func LoadHotKey() (HotKey, error) {
	cfg, err := config.Load("")
	if err != nil {
		return HotKey{}, fmt.Errorf("load companion configuration: %w", err)
	}
	return ParseHotKey(cfg.Companion.Shortcut)
}

// ParseHotKey converts a user-facing shortcut such as command+shift+k into
// Carbon registration values. A modifier is mandatory so ordinary typing can
// never be captured globally.
func ParseHotKey(value string) (HotKey, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = defaultShortcut
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '+' || r == '-' })
	if len(parts) < 2 {
		return HotKey{}, fmt.Errorf("companion shortcut %q must include a modifier and key", value)
	}

	var modifiers uint32
	var labels []string
	key := ""
	for _, raw := range parts {
		part := strings.TrimSpace(raw)
		switch part {
		case "command", "cmd":
			modifiers |= commandModifier
			labels = appendUnique(labels, "Command")
		case "shift":
			modifiers |= shiftModifier
			labels = appendUnique(labels, "Shift")
		case "option", "alt":
			modifiers |= optionModifier
			labels = appendUnique(labels, "Option")
		case "control", "ctrl":
			modifiers |= controlModifier
			labels = appendUnique(labels, "Control")
		default:
			if key != "" {
				return HotKey{}, fmt.Errorf("companion shortcut %q contains more than one key", value)
			}
			key = part
		}
	}
	if modifiers == 0 || key == "" {
		return HotKey{}, fmt.Errorf("companion shortcut %q must include a modifier and key", value)
	}
	keyCode, ok := keyCodes[key]
	if !ok {
		return HotKey{}, fmt.Errorf("companion shortcut key %q is unsupported", key)
	}
	labels = append(labels, strings.ToUpper(key))
	return HotKey{KeyCode: keyCode, Modifiers: modifiers, Label: strings.Join(labels, "-")}, nil
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}
