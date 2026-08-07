package model

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

type muxCall struct {
	name string
	args []string
	env  []string
}

type fakeMuxRunner struct {
	calls   []muxCall
	runErr  error
	runErrs []error
	out     []byte
	outErr  error
}

func (f *fakeMuxRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, muxCall{name: name, args: slices.Clone(args)})
	if len(f.runErrs) > 0 {
		err := f.runErrs[0]
		f.runErrs = f.runErrs[1:]
		return err
	}
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
		{name: "herdr", env: map[string]string{"HERDR_ENV": "1", "HERDR_WORKSPACE_ID": "w1"}, want: "herdr"},
		{name: "other herdr value is ignored", env: map[string]string{"HERDR_ENV": "true"}, want: ""},
		{name: "herdr wins when nested", env: map[string]string{"HERDR_ENV": "1", "ZELLIJ": "0", "TMUX": "/tmp/tmux"}, want: "herdr"},
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

func TestHerdrMultiplexerOpenEditor(t *testing.T) {
	tests := []struct {
		name       string
		placement  EditorPlacement
		out        string
		wantTarget string
		wantCreate []string
		wantPane   string
	}{
		{
			name:       "preferred creates reusable tab",
			placement:  EditorPreferred,
			out:        `{"result":{"tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p4"}}}`,
			wantTarget: "w1:t2",
			wantCreate: []string{"tab", "create", "--cwd", "/board", "--label", "todo/Can't wait.md", "--focus"},
			wantPane:   "w1:p4",
		},
		{
			name:       "tiled creates uncached right split",
			placement:  EditorTiled,
			out:        `{"result":{"pane":{"pane_id":"w1:p5"}}}`,
			wantCreate: []string{"pane", "split", "--current", "--direction", "right", "--cwd", "/board", "--focus"},
			wantPane:   "w1:p5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeMuxRunner{out: []byte(tt.out)}
			backend := herdrMultiplexer{runner: runner, workspaceID: "w1"}

			target, err := backend.OpenEditor("/board", "/board/todo/Can't wait.md", tt.placement)
			if err != nil {
				t.Fatal(err)
			}
			if target != tt.wantTarget {
				t.Fatalf("focus target = %q, want %q", target, tt.wantTarget)
			}
			if len(runner.calls) != 3 {
				t.Fatalf("calls = %#v, want create, pane rename, and pane run", runner.calls)
			}
			if call := runner.calls[0]; call.name != "herdr" || !slices.Equal(call.args, tt.wantCreate) {
				t.Fatalf("create call = %#v, want herdr %#v", call, tt.wantCreate)
			}
			wantRename := []string{"pane", "rename", tt.wantPane, "todo/Can't wait.md"}
			if call := runner.calls[1]; call.name != "herdr" || !slices.Equal(call.args, wantRename) {
				t.Fatalf("rename call = %#v, want herdr %#v", call, wantRename)
			}
			wantRun := []string{"pane", "run", tt.wantPane, `exec ${VISUAL:-${EDITOR:-vi}} '/board/todo/Can'"'"'t wait.md'`}
			if call := runner.calls[2]; call.name != "herdr" || !slices.Equal(call.args, wantRun) {
				t.Fatalf("run call = %#v, want herdr %#v", call, wantRun)
			}
		})
	}
}

func TestHerdrMultiplexerShellFocusAndRename(t *testing.T) {
	runner := &fakeMuxRunner{out: []byte(`{"result":{"pane":{"pane_id":"w1:p5"}}}`)}
	backend := herdrMultiplexer{runner: runner, workspaceID: "w1"}

	if err := backend.OpenShell("/board"); err != nil {
		t.Fatal(err)
	}
	if err := backend.FocusTarget("w1:t2"); err != nil {
		t.Fatal(err)
	}
	if err := backend.RenameWorkspace("roadmap"); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"pane", "split", "--current", "--direction", "right", "--cwd", "/board", "--focus"},
		{"tab", "focus", "w1:t2"},
		{"workspace", "rename", "w1", "roadmap"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	for i, args := range want {
		if runner.calls[i].name != "herdr" || !slices.Equal(runner.calls[i].args, args) {
			t.Errorf("call %d = %#v, want herdr %#v", i, runner.calls[i], args)
		}
	}
}

func TestHerdrMultiplexerCleansUpFailedEditor(t *testing.T) {
	tests := []struct {
		name        string
		placement   EditorPlacement
		out         string
		wantCleanup []string
	}{
		{
			name:        "tab",
			placement:   EditorPreferred,
			out:         `{"result":{"tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p4"}}}`,
			wantCleanup: []string{"tab", "close", "w1:t2"},
		},
		{
			name:        "split",
			placement:   EditorTiled,
			out:         `{"result":{"pane":{"pane_id":"w1:p5"}}}`,
			wantCleanup: []string{"pane", "close", "w1:p5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeMuxRunner{out: []byte(tt.out), runErrs: []error{nil, errors.New("editor failed"), nil}}
			backend := herdrMultiplexer{runner: runner, workspaceID: "w1"}
			if _, err := backend.OpenEditor("/board", "/board/card.md", tt.placement); err == nil || !strings.Contains(err.Error(), "editor failed") {
				t.Fatalf("OpenEditor() error = %v, want editor failure", err)
			}
			if call := runner.calls[len(runner.calls)-1]; call.name != "herdr" || !slices.Equal(call.args, tt.wantCleanup) {
				t.Fatalf("cleanup call = %#v, want herdr %#v", call, tt.wantCleanup)
			}
		})
	}
}

func TestHerdrMultiplexerCleansUpFailedPaneRename(t *testing.T) {
	runner := &fakeMuxRunner{
		out:     []byte(`{"result":{"pane":{"pane_id":"w1:p5"}}}`),
		runErrs: []error{errors.New("rename failed"), nil},
	}
	backend := herdrMultiplexer{runner: runner, workspaceID: "w1"}

	if _, err := backend.OpenEditor("/board", "/board/card.md", EditorTiled); err == nil || !strings.Contains(err.Error(), "rename failed") {
		t.Fatalf("OpenEditor() error = %v, want rename failure", err)
	}
	wantCleanup := []string{"pane", "close", "w1:p5"}
	if call := runner.calls[len(runner.calls)-1]; call.name != "herdr" || !slices.Equal(call.args, wantCleanup) {
		t.Fatalf("cleanup call = %#v, want herdr %#v", call, wantCleanup)
	}
}

func TestHerdrEditorLabel(t *testing.T) {
	tests := []struct {
		name     string
		boardDir string
		path     string
		want     string
	}{
		{name: "inside board", boardDir: "/board", path: "/board/todo/card.md", want: "todo/card.md"},
		{name: "outside board", boardDir: "/board", path: "/notes/card.md", want: "/notes/card.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := herdrEditorLabel(tt.boardDir, tt.path); got != tt.want {
				t.Fatalf("herdrEditorLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseHerdrCreateResponse(t *testing.T) {
	tests := []struct {
		name string
		out  string
		kind string
	}{
		{name: "malformed JSON", out: `{`, kind: "tab"},
		{name: "missing tab ID", out: `{"result":{"root_pane":{"pane_id":"w1:p1"}}}`, kind: "tab"},
		{name: "missing root pane ID", out: `{"result":{"tab":{"tab_id":"w1:t1"}}}`, kind: "tab"},
		{name: "missing split pane ID", out: `{"result":{}}`, kind: "pane"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.kind == "tab" {
				_, _, err = parseHerdrTab([]byte(tt.out))
			} else {
				_, err = parseHerdrPane([]byte(tt.out))
			}
			if err == nil {
				t.Fatal("parse response succeeded, want error")
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
		backend:       tmuxMultiplexer{runner: runner},
		boardDir:      "/board",
		path:          "/board/card.md",
		editorTargets: map[string]string{"/board/card.md": "%4"},
	}

	msg, ok := terminal.openEditor(EditorTiled)().(terminalDoneMsg)
	if !ok {
		t.Fatal("editor command returned unexpected message")
	}
	if msg.err != nil || msg.desc != "focused editor" || msg.focusTarget != "%4" {
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
		backend:       tmuxMultiplexer{runner: runner},
		boardDir:      "/board",
		path:          "/board/card.md",
		editorTargets: map[string]string{"/board/card.md": "%4"},
	}

	msg := terminal.openEditor(EditorTiled)().(terminalDoneMsg)
	if msg.err != nil || msg.focusTarget != "%9" || msg.desc != "opened tiled editor" {
		t.Fatalf("message = %#v, want replacement pane %%9", msg)
	}
	if len(runner.calls) != 2 || runner.calls[1].args[0] != "split-window" {
		t.Fatalf("calls = %#v, want focus then split", runner.calls)
	}
}

func TestTerminalHerdrEditorReuseMatchesPlacement(t *testing.T) {
	t.Run("preferred opens reusable tab", func(t *testing.T) {
		runner := &fakeMuxRunner{out: []byte(`{"result":{"tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p4"}}}`)}
		terminal := Terminal{
			backend:       herdrMultiplexer{runner: runner, workspaceID: "w1"},
			boardDir:      "/board",
			path:          "/board/card.md",
			editorTargets: map[string]string{},
		}

		msg := terminal.openEditor(EditorPreferred)().(terminalDoneMsg)
		if msg.err != nil || msg.desc != "opened editor tab" || msg.focusTarget != "w1:t2" {
			t.Fatalf("message = %#v, want reusable Herdr editor tab", msg)
		}
	})

	t.Run("preferred tab reuses focus target", func(t *testing.T) {
		runner := &fakeMuxRunner{}
		terminal := Terminal{
			backend:       herdrMultiplexer{runner: runner, workspaceID: "w1"},
			boardDir:      "/board",
			path:          "/board/card.md",
			editorTargets: map[string]string{"/board/card.md": "w1:t2"},
		}

		msg := terminal.openEditor(EditorPreferred)().(terminalDoneMsg)
		if msg.err != nil || msg.desc != "focused editor" || msg.focusTarget != "w1:t2" {
			t.Fatalf("message = %#v, want focused Herdr tab", msg)
		}
		if len(runner.calls) != 1 || !slices.Equal(runner.calls[0].args, []string{"tab", "focus", "w1:t2"}) {
			t.Fatalf("calls = %#v, want tab focus", runner.calls)
		}
	})

	t.Run("tiled editor has no reusable focus target", func(t *testing.T) {
		runner := &fakeMuxRunner{out: []byte(`{"result":{"pane":{"pane_id":"w1:p5"}}}`)}
		terminal := Terminal{
			backend:       herdrMultiplexer{runner: runner, workspaceID: "w1"},
			boardDir:      "/board",
			path:          "/board/card.md",
			editorTargets: map[string]string{},
		}

		msg := terminal.openEditor(EditorTiled)().(terminalDoneMsg)
		if msg.err != nil || msg.desc != "opened tiled editor" || msg.focusTarget != "" {
			t.Fatalf("message = %#v, want uncached tiled editor", msg)
		}
	})
}

func TestTerminalViewDescribesHerdrPreferredEditorAsTab(t *testing.T) {
	terminal := Terminal{backend: herdrMultiplexer{runner: &fakeMuxRunner{}}}
	if view := terminal.View(); !strings.Contains(view, "open editor in new tab") {
		t.Fatalf("Herdr terminal menu missing tab placement:\n%s", view)
	}
}
