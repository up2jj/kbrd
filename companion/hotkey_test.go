package companion

import (
	"strings"
	"testing"
)

func TestParseHotKey(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantCode  uint32
		wantMods  uint32
		wantLabel string
	}{
		{name: "default", value: "", wantCode: 40, wantMods: commandModifier | shiftModifier, wantLabel: "Command-Shift-K"},
		{name: "aliases", value: "ctrl-alt-space", wantCode: 49, wantMods: controlModifier | optionModifier, wantLabel: "Control-Option-SPACE"},
		{name: "function key", value: "command+f12", wantCode: 111, wantMods: commandModifier, wantLabel: "Command-F12"},
		{name: "case and whitespace", value: " CMD + Shift + P ", wantCode: 35, wantMods: commandModifier | shiftModifier, wantLabel: "Command-Shift-P"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHotKey(tt.value)
			if err != nil {
				t.Fatalf("ParseHotKey: %v", err)
			}
			if got.KeyCode != tt.wantCode || got.Modifiers != tt.wantMods || got.Label != tt.wantLabel {
				t.Fatalf("ParseHotKey(%q) = %+v, want code=%d modifiers=%d label=%q", tt.value, got, tt.wantCode, tt.wantMods, tt.wantLabel)
			}
		})
	}
}

func TestParseHotKeyRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "k", want: "modifier and key"},
		{value: "command+shift", want: "modifier and key"},
		{value: "command+home", want: "unsupported"},
		{value: "command+k+p", want: "more than one key"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			_, err := ParseHotKey(tt.value)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseHotKey(%q) error = %v, want %q", tt.value, err, tt.want)
			}
		})
	}
}
