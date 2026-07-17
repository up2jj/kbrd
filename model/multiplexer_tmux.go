package model

type tmuxMultiplexer struct {
	runner muxRunner
}

func (t tmuxMultiplexer) Name() string { return "tmux" }

func (t tmuxMultiplexer) Supports(MultiplexerCapability) bool { return false }

func (t tmuxMultiplexer) FocusPane(id string) error {
	if err := t.runner.Run("tmux", "select-window", "-t", id); err != nil {
		return err
	}
	return t.runner.Run("tmux", "select-pane", "-t", id)
}

func (t tmuxMultiplexer) OpenEditor(boardDir, path string, placement EditorPlacement) (string, error) {
	action := "new-window"
	if placement == EditorTiled {
		action = "split-window"
	}
	args := []string{
		action, "-P", "-F", "#{pane_id}", "-c", boardDir,
		"sh", "-c", `exec ${VISUAL:-${EDITOR:-vi}} "$1"`, "kbrd-editor", path,
	}
	out, err := t.runner.Output("tmux", args, nil)
	if err != nil {
		return "", err
	}
	return parsePaneID(out), nil
}

func (t tmuxMultiplexer) OpenShell(boardDir string) error {
	return t.runner.Run("tmux", "split-window", "-c", boardDir)
}

func (t tmuxMultiplexer) RenameWorkspace(name string) error {
	return t.runner.Run("tmux", "rename-window", name)
}
