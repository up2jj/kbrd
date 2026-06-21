package model

import (
	"testing"

	"kbrd/config"
)

func TestBoardMnemonics_BuildsStableItemRefs(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha"})
	colB := newTestColumn(t, map[string]string{"a": "bravo"})
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{colA, colB}

	b.mnemonics().rebuild()
	tag := b.mnemonics().lookup(1)("a")
	if tag == "" {
		t.Fatal("expected mnemonic for colB/a")
	}
	ref := b.refByMnemonic[tag]

	b.columns = []*Column{colB, colA}
	col, item, err := b.resolveDelayedItemRef(ref)
	if err != nil {
		t.Fatalf("resolve mnemonic ref: %v", err)
	}
	if col != colB || item.Name != "a" {
		t.Fatalf("resolved mnemonic to col=%q item=%q, want colB/a", col.Name, item.Name)
	}
}
