package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/config"
	"kbrd/events"
)

func TestMnemonicSelector_OpensViaColon(t *testing.T) {
	b := boardWithNCols(t, 1, 1)

	_, _ = b.handleBoardKey(keyPressText(":"))

	if !b.mnemonic.active {
		t.Fatal("mnemonic selector did not open")
	}
}

func TestMnemonicSelector_PartialMnemonicStaysOpenThroughDebounce(t *testing.T) {
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
	before := b.columns[0].SelectedItem().Name
	b.mnemonicSelector().open()
	_, _ = b.mnemonicSelector().handleKey(keyPressText(tag[:1]))
	_, cmd := b.mnemonicSelector().handleDebounce(mnemonicDebounceMsg{Seq: b.mnemonic.seq})

	if cmd != nil {
		t.Fatalf("partial mnemonic returned unexpected command: %T", cmd)
	}
	if !b.mnemonic.active {
		t.Fatal("mnemonic selector closed on partial mnemonic debounce")
	}
	if got := b.mnemonic.input.Value(); got != tag[:1] {
		t.Fatalf("mnemonic input = %q, want %q", got, tag[:1])
	}
	if got := b.columns[0].SelectedItem().Name; got != before {
		t.Fatalf("selected item changed before complete mnemonic: got %q, want %q", got, before)
	}
}

func TestMnemonicSelector_UnknownMnemonicClosesAndClears(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{"a": "a"})}
	b.rebuildMnemonics()

	b.mnemonicSelector().open()
	b.mnemonic.input.SetValue("z")
	_, cmd := b.mnemonicSelector().handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if b.mnemonic.active {
		t.Fatal("mnemonic selector stayed open after unknown mnemonic")
	}
	if got := b.mnemonic.input.Value(); got != "" {
		t.Fatalf("mnemonic input = %q, want cleared", got)
	}
	if cmd == nil {
		t.Fatal("expected error notification command")
	}
}

func TestMnemonicSelector_DebounceSelectsMatchingCardInAnotherColumn(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{colA, colB}
	b.rebuildMnemonics()

	tag := b.mnemonicLookup(1)("a")
	if tag == "" {
		t.Fatal("expected mnemonic for colB/a")
	}

	b.selectedCol = 0
	b.mnemonicSelector().open()
	_, _ = b.mnemonicSelector().handleKey(keyPressText(tag))
	_, cmd := b.mnemonicSelector().handleDebounce(mnemonicDebounceMsg{Seq: b.mnemonic.seq})

	if cmd != nil {
		t.Fatalf("valid mnemonic debounce returned unexpected command: %T", cmd)
	}
	if b.mnemonic.active {
		t.Fatal("mnemonic selector stayed open after debounced jump")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if got := colB.SelectedItem().Name; got != "a" {
		t.Fatalf("selected item = %q, want a", got)
	}
}

func TestMnemonicSelector_StaleDebounceDoesNotSelect(t *testing.T) {
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
	before := b.columns[0].SelectedItem().Name
	b.mnemonicSelector().open()
	_, _ = b.mnemonicSelector().handleKey(keyPressText(tag[:1]))
	staleSeq := b.mnemonic.seq
	_, _ = b.mnemonicSelector().handleKey(keyPressText(tag[1:]))

	_, cmd := b.mnemonicSelector().handleDebounce(mnemonicDebounceMsg{Seq: staleSeq})

	if cmd != nil {
		t.Fatalf("stale debounce returned unexpected command: %T", cmd)
	}
	if !b.mnemonic.active {
		t.Fatal("stale debounce closed mnemonic selector")
	}
	if got := b.columns[0].SelectedItem().Name; got != before {
		t.Fatalf("stale debounce changed selection: got %q, want %q", got, before)
	}
}

func TestMnemonicSelector_UnknownMnemonicDebounceClosesAndClears(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{"a": "a"})}
	b.rebuildMnemonics()

	b.mnemonicSelector().open()
	_, _ = b.mnemonicSelector().handleKey(keyPressText("z"))
	_, cmd := b.mnemonicSelector().handleDebounce(mnemonicDebounceMsg{Seq: b.mnemonic.seq})

	if b.mnemonic.active {
		t.Fatal("mnemonic selector stayed open after unknown debounce")
	}
	if got := b.mnemonic.input.Value(); got != "" {
		t.Fatalf("mnemonic input = %q, want cleared", got)
	}
	if cmd == nil {
		t.Fatal("expected error notification command")
	}
}

func TestMnemonicSelector_EnterSelectsMatchingCardInAnotherColumn(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{colA, colB}
	b.rebuildMnemonics()

	tag := b.mnemonicLookup(1)("a")
	if tag == "" {
		t.Fatal("expected mnemonic for colB/a")
	}

	b.selectedCol = 0
	b.mnemonicSelector().open()
	b.mnemonic.input.SetValue(tag)
	_, cmd := b.mnemonicSelector().handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("valid mnemonic returned unexpected command: %T", cmd)
	}
	if b.mnemonic.active {
		t.Fatal("mnemonic selector stayed open after jump")
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if got := colB.SelectedItem().Name; got != "a" {
		t.Fatalf("selected item = %q, want a", got)
	}
}

func TestMnemonicSelector_OpensFromFocusedVirtualColumn(t *testing.T) {
	b := boardWithNCols(t, 1, 2)
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{
		Name:     "Tasks",
		Items:    []events.VirtualItem{{ID: "a", Title: "Alpha"}},
		Commands: []events.VirtualCommand{{ID: "colon", Name: "Colon", Key: ":", Ref: "vcol:tasks:colon"}},
	})
	b.selectedCol = 1

	_, cmd := b.handleBoardKey(keyPressText(":"))

	if cmd == nil {
		t.Fatal("expected focus command for mnemonic input")
	}
	if !b.mnemonic.active {
		t.Fatal("mnemonic selector did not open from virtual column")
	}
}

func TestMnemonicSelector_EnterSelectsVirtualCardByMnemonic(t *testing.T) {
	b := boardWithNCols(t, 1, 2)
	b.setVirtualColumn("tasks", events.VirtualColumnSpec{
		Name: "Tasks",
		Items: []events.VirtualItem{
			{ID: "a", Title: "Alpha", Path: "/tmp/alpha.md"},
			{ID: "b", Title: "Beta", Path: "/tmp/beta.md"},
		},
	})
	b.rebuildMnemonics()
	vc := b.columns[1]
	tag := b.mnemonicLookup(1)("b")
	if tag == "" {
		t.Fatal("expected mnemonic for virtual item b")
	}

	b.selectedCol = 0
	vc.SelectByName("a")
	b.mnemonicSelector().open()
	b.mnemonic.input.SetValue(tag)
	_, cmd := b.mnemonicSelector().handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("valid virtual mnemonic returned unexpected command: %T", cmd)
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want virtual column index 1", b.selectedCol)
	}
	if got := vc.SelectedItem().Name; got != "b" {
		t.Fatalf("selected virtual item = %q, want b", got)
	}
}

func TestMnemonicSelector_UsesStableRefAfterColumnReorder(t *testing.T) {
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
	b.selectedCol = 1
	b.mnemonicSelector().open()
	b.mnemonic.input.SetValue(tag)
	_, cmd := b.mnemonicSelector().handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("valid mnemonic returned unexpected command: %T", cmd)
	}
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol = %d, want reordered target index 0", b.selectedCol)
	}
	if got := colB.SelectedItem().Name; got != "a" {
		t.Fatalf("selected item = %q, want a", got)
	}
}

func TestMnemonicSelector_EscapeClosesAndClearsWithoutSelectionChange(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{"a": "a", "b": "b"})}
	b.rebuildMnemonics()
	b.columns[0].SelectByName("b")

	b.mnemonicSelector().open()
	b.mnemonic.input.SetValue("a")
	_, cmd := b.mnemonicSelector().handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})

	if cmd != nil {
		t.Fatalf("escape returned unexpected command: %T", cmd)
	}
	if b.mnemonic.active {
		t.Fatal("mnemonic selector stayed open after escape")
	}
	if got := b.mnemonic.input.Value(); got != "" {
		t.Fatalf("mnemonic input = %q, want cleared", got)
	}
	if got := b.columns[0].SelectedItem().Name; got != "b" {
		t.Fatalf("selected item changed after escape: got %q, want b", got)
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
