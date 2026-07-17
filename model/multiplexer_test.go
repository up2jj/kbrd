package model

import (
	"errors"
	"slices"
	"testing"
)

type muxCall struct {
	name string
	args []string
	env  []string
}

type fakeMuxRunner struct {
	calls  []muxCall
	runErr error
	out    []byte
	outErr error
}

func (f *fakeMuxRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, muxCall{name: name, args: slices.Clone(args)})
	return f.runErr
}

func (f *fakeMuxRunner) Output(name string, args []string, env []string) ([]byte, error) {
	f.calls = append(f.calls, muxCall{name: name, args: slices.Clone(args), env: slices.Clone(env)})
	return f.out, f.outErr
}

func TestDetectMultiplexer(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "zellij", env: map[string]string{"ZELLIJ": "0"}, want: "zellij"},
		{name: "tmux", env: map[string]string{"TMUX": "/tmp/tmux"}, want: "tmux"},
		{name: "zellij wins when nested", env: map[string]string{"ZELLIJ": "0", "TMUX": "/tmp/tmux"}, want: "zellij"},
		{name: "plain terminal", env: map[string]string{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := detectMultiplexer(func(key string) string { return tt.env[key] }, &fakeMuxRunner{})
			if backend == nil {
				if tt.want != "" {
					t.Fatalf("detectMultiplexer() = nil, want %s", tt.want)
				}
				return
			}
			if got := backend.Name(); got != tt.want {
				t.Fatalf("detectMultiplexer().Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZellijMultiplexerOpenEditor(t *testing.T) {
	t.Setenv("VISUAL", "hx")
	runner := &fakeMuxRunner{out: []byte("pane id 12\n")}
	backend := zellijMultiplexer{runner: runner}

	id, err := backend.OpenEditor("/board", "/board/todo/card.md", EditorPreferred)
	if err != nil {
		t.Fatal(err)
	}
	if id != "12" {
		t.Fatalf("pane id = %q, want 12", id)
	}
	wantArgs := []string{"edit", "--cwd", "/board", "-f", "/board/todo/card.md"}
	if len(runner.calls) != 1 || runner.calls[0].name != "zellij" || !slices.Equal(runner.calls[0].args, wantArgs) {
		t.Fatalf("call = %#v, want zellij %#v", runner.calls, wantArgs)
	}
	if !slices.Contains(runner.calls[0].env, "EDITOR=hx") || !slices.Contains(runner.calls[0].env, "VISUAL=hx") {
		t.Fatalf("editor environment missing from %#v", runner.calls[0].env)
	}
}

func TestTmuxMultiplexerEditorPlacement(t *testing.T) {
	tests := []struct {
		name      string
		placement EditorPlacement
		action    string
	}{
		{name: "preferred opens window", placement: EditorPreferred, action: "new-window"},
		{name: "tiled opens split", placement: EditorTiled, action: "split-window"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeMuxRunner{out: []byte("%7\n")}
			backend := tmuxMultiplexer{runner: runner}
			id, err := backend.OpenEditor("/board", "/board/card.md", tt.placement)
			if err != nil {
				t.Fatal(err)
			}
			if id != "%7" {
				t.Fatalf("pane id = %q, want %%7", id)
			}
			call := runner.calls[0]
			if call.name != "tmux" || call.args[0] != tt.action {
				t.Fatalf("call = %#v, want tmux %s", call, tt.action)
			}
			if !slices.Contains(call.args, "/board") || !slices.Contains(call.args, "/board/card.md") {
				t.Fatalf("tmux call lost cwd or path: %#v", call.args)
			}
		})
	}
}

func TestTerminalReusesLiveEditorPane(t *testing.T) {
	runner := &fakeMuxRunner{}
	terminal := Terminal{
		backend:     tmuxMultiplexer{runner: runner},
		boardDir:    "/board",
		path:        "/board/card.md",
		editorPanes: map[string]string{"/board/card.md": "%4"},
	}

	msg, ok := terminal.openEditor(EditorTiled)().(terminalDoneMsg)
	if !ok {
		t.Fatal("editor command returned unexpected message")
	}
	if msg.err != nil || msg.desc != "focused editor" || msg.paneID != "%4" {
		t.Fatalf("message = %#v, want focused pane %%4", msg)
	}
	wantWindow := []string{"select-window", "-t", "%4"}
	wantPane := []string{"select-pane", "-t", "%4"}
	if len(runner.calls) != 2 || !slices.Equal(runner.calls[0].args, wantWindow) || !slices.Equal(runner.calls[1].args, wantPane) {
		t.Fatalf("calls = %#v, want window and pane focus", runner.calls)
	}
}

func TestTerminalReplacesStaleEditorPane(t *testing.T) {
	runner := &fakeMuxRunner{runErr: errors.New("missing pane"), out: []byte("%9\n")}
	terminal := Terminal{
		backend:     tmuxMultiplexer{runner: runner},
		boardDir:    "/board",
		path:        "/board/card.md",
		editorPanes: map[string]string{"/board/card.md": "%4"},
	}

	msg := terminal.openEditor(EditorTiled)().(terminalDoneMsg)
	if msg.err != nil || msg.paneID != "%9" || msg.desc != "opened tiled editor" {
		t.Fatalf("message = %#v, want replacement pane %%9", msg)
	}
	if len(runner.calls) != 2 || runner.calls[1].args[0] != "split-window" {
		t.Fatalf("calls = %#v, want focus then split", runner.calls)
	}
}
