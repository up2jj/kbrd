package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"kbrd/board"
	"kbrd/recents"
)

func TestReadBoardPromptsFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, boardPromptsFile)
	data := `
prompts:
  - name: weekly_review
    title: Weekly review
    description: Review this board
    arguments:
      - name: focus
        description: Area to emphasize
      - name: column
        description: Column to review
    content: |
      Review {{.boardName}} at {{.boardPath}}. Focus: {{.focus}}
  - name: handoff
    messages:
      - role: user
        content: Summarize {{.boardName}}.
      - role: assistant
        content: I will inspect the board first.
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	definitions, warnings, err := readBoardPromptsFile(path, board.Ref{Name: "Work", Path: root})
	if err != nil {
		t.Fatalf("read prompts: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(definitions) != 2 {
		t.Fatalf("definitions = %d, want 2", len(definitions))
	}
	if got := definitions[0].registeredName(); got != "work__weekly_review" {
		t.Fatalf("registered name = %q", got)
	}

	request := &sdkmcp.GetPromptRequest{Params: &sdkmcp.GetPromptParams{Arguments: map[string]string{"focus": "blocked cards"}}}
	result, err := definitions[0].handler()(t.Context(), request)
	if err != nil {
		t.Fatalf("render prompt: %v", err)
	}
	text := result.Messages[0].Content.(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "Review Work at "+root) || !strings.Contains(text, "blocked cards") {
		t.Fatalf("rendered text = %q", text)
	}
}

func TestReadBoardPromptsFileReportsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, boardPromptsFile)
	data := `
prompts:
  - name: valid
    content: Hello
  - name: bad name
    content: Hello
  - name: missing_content
  - name: valid
    content: Duplicate
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	definitions, warnings, err := readBoardPromptsFile(path, board.Ref{Path: root})
	if err != nil {
		t.Fatalf("read prompts: %v", err)
	}
	if len(definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(definitions))
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want 3", warnings)
	}
}

func TestPromptsProtocol(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Doing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, boardPromptsFile), []byte(`
prompts:
  - name: weekly_review
    title: Weekly review
    arguments:
      - name: focus
        required: true
      - name: column
    content: Review {{.boardName}}, focusing on {{.focus}}.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedRecents(t, []recents.Entry{{Path: root, Name: "Work"}})

	server := newServer(Policy{})
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer session.Close()

	listed, err := session.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	want := map[string]bool{
		"board_summary": false, "board_triage": false, "plan_board_work": false, "work__weekly_review": false,
	}
	for _, prompt := range listed.Prompts {
		if _, ok := want[prompt.Name]; ok {
			want[prompt.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("prompt %q not advertised", name)
		}
	}

	completion, err := session.Complete(t.Context(), &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/prompt", Name: "board_summary"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "board", Value: "Wo"},
	})
	if err != nil {
		t.Fatalf("complete prompt board: %v", err)
	}
	if len(completion.Completion.Values) != 1 || completion.Completion.Values[0] != "Work" {
		t.Fatalf("board completion = %v", completion.Completion.Values)
	}
	completion, err = session.Complete(t.Context(), &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/prompt", Name: "board_summary"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "column", Value: "Do"},
		Context:  &sdkmcp.CompleteContext{Arguments: map[string]string{"board": "Work"}},
	})
	if err != nil {
		t.Fatalf("complete built-in prompt column: %v", err)
	}
	if len(completion.Completion.Values) != 1 || completion.Completion.Values[0] != "Doing" {
		t.Fatalf("built-in column completion = %v", completion.Completion.Values)
	}
	completion, err = session.Complete(t.Context(), &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/prompt", Name: "work__weekly_review"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "column", Value: "Do"},
	})
	if err != nil {
		t.Fatalf("complete board prompt column: %v", err)
	}
	if len(completion.Completion.Values) != 1 || completion.Completion.Values[0] != "Doing" {
		t.Fatalf("board prompt column completion = %v", completion.Completion.Values)
	}
	builtIn, err := session.GetPrompt(t.Context(), &sdkmcp.GetPromptParams{
		Name: "board_summary", Arguments: map[string]string{"board": "Work", "column": "Doing"},
	})
	if err != nil {
		t.Fatalf("get built-in column prompt: %v", err)
	}
	builtInText := builtIn.Messages[0].Content.(*sdkmcp.TextContent).Text
	if !strings.Contains(builtInText, `limited to column "Doing"`) {
		t.Fatalf("built-in prompt text = %q", builtInText)
	}

	result, err := session.GetPrompt(t.Context(), &sdkmcp.GetPromptParams{
		Name: "work__weekly_review", Arguments: map[string]string{"focus": "delivery"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	text := result.Messages[0].Content.(*sdkmcp.TextContent).Text
	if text != "Review Work, focusing on delivery." {
		t.Fatalf("prompt text = %q", text)
	}

	if _, err := session.GetPrompt(t.Context(), &sdkmcp.GetPromptParams{Name: "work__weekly_review"}); err == nil {
		t.Fatal("expected missing required argument error")
	}
}
