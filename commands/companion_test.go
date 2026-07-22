package commands

import (
	"strings"
	"testing"

	"kbrd/scratchpad"
)

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
