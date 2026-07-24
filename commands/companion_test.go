package commands

import (
	"bytes"
	"strings"
	"testing"

	"kbrd/scratchpad"
)

func TestCompanionHotKeyPrintsNativeSettings(t *testing.T) {
	isolateConfig(t)
	var output bytes.Buffer
	cmd := newCompanionHotKeyCmd()
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"key_code":40`, `"modifiers":768`, `"label":"Command-Shift-K"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestCompanionScratchpadReadsTextFromStdin(t *testing.T) {
	isolateConfig(t)
	boardPath := makeIngestBoard(t, "todo")
	cmd := newCompanionScratchpadCmd()
	cmd.SetArgs([]string{"--board", boardPath})
	cmd.SetIn(strings.NewReader("private note"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	store, err := scratchpad.Open("")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "private note" {
		t.Fatalf("scratchpad = %q, want %q", got, "private note")
	}
}
