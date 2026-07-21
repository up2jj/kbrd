package mcp

import (
	"strings"
	"testing"
)

func TestAgentsMarkdown(t *testing.T) {
	doc := string(AgentsMarkdown())
	if strings.Contains(doc, "§") {
		t.Fatal("sentinel § left unsubstituted in AGENTS.md")
	}
	// Spot-check that every tool is documented so the doc can't silently drift
	// out of date with the registered tools.
	for _, tool := range []string{
		"add_file_to_board", "list_boards", "list_folders", "list_files",
		"list_custom_commands", "run_custom_command",
	} {
		if !strings.Contains(doc, tool) {
			t.Errorf("AGENTS.md does not mention tool %q", tool)
		}
	}
	if !strings.Contains(doc, ServerName) {
		t.Errorf("AGENTS.md does not mention server name %q", ServerName)
	}
	for _, uri := range []string{"kbrd://boards", "kbrd://board/{board}", "kbrd://card/{board}/{column}/{card}"} {
		if !strings.Contains(doc, uri) {
			t.Errorf("AGENTS.md does not mention resource %q", uri)
		}
	}
}

func TestServerInstructions(t *testing.T) {
	doc := ServerInstructions()
	if strings.Contains(doc, "§") {
		t.Fatal("sentinel § left unsubstituted in server instructions")
	}
	for _, phrase := range []string{
		"list_boards",
		"multiple boards",
		"frontmatter",
		"Do not move a card",
		"destructive",
		"kbrd://boards",
		"elicit",
	} {
		if !strings.Contains(doc, phrase) {
			t.Errorf("server instructions do not mention %q", phrase)
		}
	}
}
