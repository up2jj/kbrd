package model

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
	"kbrd/events"
	"kbrd/script"
)

// initScripting creates the Lua host (if enabled and init files exist) and
// subscribes it to the event bus. Idempotent: a second call closes the
// previous host first, which is what board-switching needs.
func (b *Board) initScripting() {
	if b.scripts != nil {
		b.scripts.Close()
		b.scripts = nil
	}
	b.bus = events.Bus{}

	if !b.cfg.Scripting.Enabled {
		return
	}
	logger := script.NewFileLogger()
	host, err := script.New(b.cfg.Scripting, boardScriptAPI{b: b}, logger, b.cfg.Path)
	if err != nil && host == nil {
		// Hard failure during init — surface but keep running.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
		return
	}
	if host == nil {
		return
	}
	if err != nil {
		// Partial failure — some files loaded, others errored.
		b.commandWarnings = append(b.commandWarnings, config.CommandLoadWarning{
			Source:  "init.lua",
			Message: err.Error(),
		})
	}
	b.scripts = host
	b.bus.Subscribe(host)
}

// boardScriptAPI is the events.BoardAPI implementation handed to the Lua
// host. It must remain safe to call while h.mu is held inside the host —
// so it never calls back into the host itself.
type boardScriptAPI struct {
	b *Board
}

// handleScriptResult turns the (req, err) tuple from a Lua command/resume
// call into a tea.Cmd: open the matching UI on a yield, fire a finished msg
// on completion or error.
func (b *Board) handleScriptResult(name string, req *script.UIRequest, err error) tea.Cmd {
	if err != nil {
		return func() tea.Msg {
			return customCommandFinishedMsg{Name: name, Err: err}
		}
	}
	if req == nil {
		// Coroutine ran to completion.
		return func() tea.Msg {
			return customCommandFinishedMsg{Name: name, Err: nil}
		}
	}
	return b.openScriptUI(name, req)
}

// openScriptUI installs the appropriate UI state for a yielded UI request.
// Confirms reuse the existing Dialog primitive; pick and prompt use ScriptUI.
func (b *Board) openScriptUI(name string, req *script.UIRequest) tea.Cmd {
	switch req.Kind {
	case "pick":
		b.scriptUI.OpenPicker(name, req.Token, req.Title, req.Choices)
		return nil
	case "prompt":
		b.scriptUI.OpenPrompt(name, req.Token, req.Title, req.Default)
		return nil
	case "confirm":
		title := req.Title
		if title == "" {
			title = "Confirm?"
		}
		b.dialog.Open(DialogOptions{
			Title: title,
			Buttons: []DialogButton{
				{Label: "Yes", Kind: ButtonPrimary,
					Msg: scriptResumeMsg{Name: name, Token: req.Token, Result: true}},
				{Label: "No",
					Msg: scriptResumeMsg{Name: name, Token: req.Token, Result: false}},
			},
			DefaultIndex: 0,
		})
		return nil
	}
	// Unknown UI kind — best-effort: resume with nil so the script doesn't hang.
	return func() tea.Msg {
		return scriptResumeMsg{Name: name, Token: req.Token, Result: nil}
	}
}

// handleScriptResume re-enters a suspended coroutine with the user's answer.
// If it yields again (chained UI calls), open the next UI; if it finishes,
// emit a customCommandFinished.
func (b *Board) handleScriptResume(msg scriptResumeMsg) (tea.Model, tea.Cmd) {
	req, err := b.scripts.ResumeWith(msg.Token, msg.Result)
	return b, b.handleScriptResult(msg.Name, req, err)
}

func (a boardScriptAPI) Notify(msg, level string) {
	sev := notifySuccess
	if level == "error" {
		sev = notifyError
	}
	a.b.notifier.fire(msg, sev)
}

// resolve returns path as-is if absolute, otherwise joined against the
// board root. All kbrd.fs.* methods funnel through here so behavior is
// consistent and predictable for scripts that pass in short names.
func (a boardScriptAPI) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.b.cfg.Path, path)
}

func (a boardScriptAPI) FSRead(path string) (string, error) {
	data, err := os.ReadFile(a.resolve(path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a boardScriptAPI) FSWrite(path, body string) error {
	return os.WriteFile(a.resolve(path), []byte(body), 0o644)
}

func (a boardScriptAPI) FSExists(path string) bool {
	_, err := os.Stat(a.resolve(path))
	return err == nil
}

func (a boardScriptAPI) FSMkdir(path string) error {
	return os.MkdirAll(a.resolve(path), 0o755)
}

func (a boardScriptAPI) FSGlob(pattern string) ([]string, error) {
	return filepath.Glob(a.resolve(pattern))
}

func (a boardScriptAPI) Refresh() error {
	if err := a.b.loadColumns(); err != nil {
		return err
	}
	a.b.refreshGitStats()
	return nil
}

func (a boardScriptAPI) CreateColumn(name string) error {
	if err := validateRenameName(name); err != nil {
		return err
	}
	dir := filepath.Join(a.b.cfg.Path, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("column %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return a.Refresh()
}

func (a boardScriptAPI) MoveItem(item events.ItemRef, toColumn string) error {
	var src, dst *Column
	for _, c := range a.b.columns {
		if c.Name == item.Column {
			src = c
		}
		if c.Name == toColumn {
			dst = c
		}
	}
	if src == nil {
		return fmt.Errorf("source column %q not found", item.Column)
	}
	if dst == nil {
		return fmt.Errorf("destination column %q not found", toColumn)
	}
	if err := src.MoveItemTo(dst, item.Name); err != nil {
		return err
	}
	a.b.bus.Publish(events.ItemMoved{
		Item: item,
		From: item.Column,
		To:   toColumn,
	})
	return nil
}
