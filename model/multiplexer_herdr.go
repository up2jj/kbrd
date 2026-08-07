package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type herdrMultiplexer struct {
	runner      muxRunner
	workspaceID string
}

func (h herdrMultiplexer) Name() string { return "herdr" }

func (h herdrMultiplexer) Supports(capability MultiplexerCapability) bool {
	return capability == TabbedEditors
}

func (h herdrMultiplexer) FocusTarget(id string) error {
	return h.runner.Run("herdr", "tab", "focus", id)
}

func (h herdrMultiplexer) OpenEditor(boardDir, path string, placement EditorPlacement) (string, error) {
	if placement == EditorPreferred {
		return h.openEditorTab(boardDir, path)
	}
	return h.openEditorSplit(boardDir, path)
}

func (h herdrMultiplexer) openEditorTab(boardDir, path string) (string, error) {
	label := herdrEditorLabel(boardDir, path)
	out, err := h.runner.Output("herdr", []string{"tab", "create", "--cwd", boardDir, "--label", label, "--focus"}, nil)
	if err != nil {
		return "", fmt.Errorf("creating editor tab: %w", err)
	}
	tabID, paneID, err := parseHerdrTab(out)
	if err != nil {
		return "", err
	}
	if err := h.startEditor(paneID, label, path); err != nil {
		return "", errors.Join(
			err,
			herdrCleanupError("tab", tabID, h.runner.Run("herdr", "tab", "close", tabID)),
		)
	}
	return tabID, nil
}

func (h herdrMultiplexer) openEditorSplit(boardDir, path string) (string, error) {
	paneID, err := h.createSplit(boardDir)
	if err != nil {
		return "", err
	}
	if err := h.startEditor(paneID, herdrEditorLabel(boardDir, path), path); err != nil {
		return "", errors.Join(
			err,
			herdrCleanupError("pane", paneID, h.runner.Run("herdr", "pane", "close", paneID)),
		)
	}
	// Herdr cannot directly focus an arbitrary ordinary pane by ID, so tiled
	// editors deliberately do not return a reusable focus target.
	return "", nil
}

func (h herdrMultiplexer) startEditor(paneID, label, path string) error {
	if err := h.runner.Run("herdr", "pane", "rename", paneID, label); err != nil {
		return fmt.Errorf("naming editor pane: %w", err)
	}
	if err := h.runner.Run("herdr", "pane", "run", paneID, herdrEditorCommand(path)); err != nil {
		return fmt.Errorf("starting editor: %w", err)
	}
	return nil
}

func (h herdrMultiplexer) OpenShell(boardDir string) error {
	_, err := h.createSplit(boardDir)
	return err
}

func (h herdrMultiplexer) createSplit(boardDir string) (string, error) {
	out, err := h.runner.Output("herdr", []string{
		"pane", "split", "--current", "--direction", "right", "--cwd", boardDir, "--focus",
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating pane split: %w", err)
	}
	return parseHerdrPane(out)
}

func (h herdrMultiplexer) RenameWorkspace(name string) error {
	if h.workspaceID == "" {
		return errors.New("HERDR_WORKSPACE_ID is empty")
	}
	return h.runner.Run("herdr", "workspace", "rename", h.workspaceID, name)
}

type herdrCreateResponse struct {
	Result struct {
		Tab struct {
			ID string `json:"tab_id"`
		} `json:"tab"`
		RootPane struct {
			ID string `json:"pane_id"`
		} `json:"root_pane"`
		Pane struct {
			ID string `json:"pane_id"`
		} `json:"pane"`
	} `json:"result"`
}

func parseHerdrTab(out []byte) (string, string, error) {
	var response herdrCreateResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return "", "", fmt.Errorf("decoding herdr tab response: %w", err)
	}
	if response.Result.Tab.ID == "" || response.Result.RootPane.ID == "" {
		return "", "", errors.New("herdr tab response is missing tab or root pane ID")
	}
	return response.Result.Tab.ID, response.Result.RootPane.ID, nil
}

func parseHerdrPane(out []byte) (string, error) {
	var response herdrCreateResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("decoding herdr pane response: %w", err)
	}
	if response.Result.Pane.ID == "" {
		return "", errors.New("herdr pane response is missing pane ID")
	}
	return response.Result.Pane.ID, nil
}

func herdrEditorCommand(path string) string {
	return `exec ${VISUAL:-${EDITOR:-vi}} ` + shellQuote(path)
}

func herdrEditorLabel(boardDir, path string) string {
	rel, err := filepath.Rel(boardDir, path)
	if err != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func herdrCleanupError(kind, id string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cleaning up editor %s %s: %w", kind, id, err)
}
