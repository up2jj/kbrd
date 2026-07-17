package model

import "os"

type zellijMultiplexer struct {
	runner muxRunner
}

func (z zellijMultiplexer) Name() string { return "zellij" }

func (z zellijMultiplexer) Supports(capability MultiplexerCapability) bool {
	return capability == FloatingPanes
}

func (z zellijMultiplexer) FocusPane(id string) error {
	return z.runner.Run("zellij", "action", "focus-pane-id", id)
}

func (z zellijMultiplexer) OpenEditor(boardDir, path string, placement EditorPlacement) (string, error) {
	args := []string{"edit", "--cwd", boardDir}
	if placement == EditorPreferred {
		args = append(args, "-f")
	}
	args = append(args, path)
	editor := resolveEditor()
	out, err := z.runner.Output("zellij", args, append(os.Environ(), "EDITOR="+editor, "VISUAL="+editor))
	if err != nil {
		return "", err
	}
	return parsePaneID(out), nil
}

func (z zellijMultiplexer) OpenShell(boardDir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	return z.runner.Run("zellij", "run", "--cwd", boardDir, "--", shell)
}

func (z zellijMultiplexer) RenameWorkspace(name string) error {
	return z.runner.Run("zellij", "action", "rename-tab", name)
}
