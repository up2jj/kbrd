package model

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

func TestQuickCommands_PartialMnemonicStaysOpen(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{
		"a": "a",
		"b": "b",
		"c": "c",
		"d": "d",
		"e": "e",
		"f": "f",
		"g": "g",
		"h": "h",
		"i": "i",
		"j": "j",
	})}
	b.rebuildMnemonics()

	tag := firstLongMnemonic(t, b)
	b.quickCommands().open()
	_, _ = b.quickCommands().handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e" + tag[:1])})

	if !b.quickCmdMode {
		t.Fatal("quick command mode closed on partial mnemonic")
	}
	if got := b.quickCmdInput.Value(); got != "e"+tag[:1] {
		t.Fatalf("quick command input = %q, want %q", got, "e"+tag[:1])
	}
}

func TestQuickCommands_UnknownMnemonicClosesAndClears(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{"a": "a"})}
	b.rebuildMnemonics()

	b.quickCommands().open()
	_, cmd := b.quickCommands().handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ez")})

	if b.quickCmdMode {
		t.Fatal("quick command mode stayed open after unknown mnemonic")
	}
	if got := b.quickCmdInput.Value(); got != "" {
		t.Fatalf("quick command input = %q, want cleared", got)
	}
	if cmd == nil {
		t.Fatal("expected error notification command")
	}
}

func TestQuickCommands_ConfirmItemCommandUsesStableRef(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{colA, colB}
	b.rebuildMnemonics()

	tag := b.mnemonicLookup(1)("a")
	if tag == "" {
		t.Fatal("expected mnemonic for colB/a")
	}

	b.columns = []*Column{colB, colA}
	b.quickCommands().open()
	b.quickCmdInput.SetValue("e" + tag)
	_, cmd := b.quickCommands().handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected editor command")
	}
	if b.editor.ColPath != colB.Path {
		t.Fatalf("editor ColPath = %q, want stable target %q", b.editor.ColPath, colB.Path)
	}
}

func firstLongMnemonic(t *testing.T, b *Board) string {
	t.Helper()
	for tag := range b.refByMnemonic {
		if len(tag) > 1 && strings.HasPrefix(tag, tag[:1]) {
			return tag
		}
	}
	t.Fatal("expected at least one multi-character mnemonic")
	return ""
}
