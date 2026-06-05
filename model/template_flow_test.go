package model

import (
	"testing"

	"github.com/atotto/clipboard"

	"kbrd/template"
)

func TestFieldSeed(t *testing.T) {
	// No prefill: the default seeds the field.
	f := template.Field{Type: "input", Default: "dflt"}
	if got := fieldSeed(f); got != "dflt" {
		t.Errorf("default seed = %q", got)
	}

	// prefill: clipboard — exercised only where a clipboard exists (skipped
	// in headless CI). The form must start with the clipboard's content.
	// Save and restore the user's clipboard around the check.
	f = template.Field{Type: "input", Prefill: template.PrefillClipboard}
	saved, savedErr := clipboard.ReadAll()
	if err := clipboard.WriteAll("from-clipboard"); err != nil {
		t.Skipf("no clipboard available: %v", err)
	}
	if savedErr == nil {
		defer func() { _ = clipboard.WriteAll(saved) }()
	}
	if got := fieldSeed(f); got != "from-clipboard" {
		t.Errorf("clipboard seed = %q", got)
	}
}
