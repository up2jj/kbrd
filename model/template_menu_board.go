package model

import (
	"errors"
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"kbrd/board"
	"kbrd/template"
)

type templateMenuActions struct {
	board *Board
}

type templateRemoveConfirmMsg struct {
	Column columnRef
	Path   string
	Name   string
	Scope  string
}

func (b *Board) templateMenuActions() templateMenuActions {
	return templateMenuActions{board: b}
}

func (a templateMenuActions) open(col *Column) (tea.Model, tea.Cmd) {
	b := a.board
	if col.Virtual {
		return b, b.notifier.ErrorCause("", errVirtualColumn)
	}
	tmpls, warns, err := template.List(b.cfg.Path, col.Path)
	if err != nil {
		return b, b.notifier.ErrorCause("failed to list templates", err)
	}
	var warnCmd tea.Cmd
	if len(warns) > 0 {
		w := warns[0]
		warnCmd = b.notifier.ErrorCause("skipped "+filepath.Base(w.Path), w.Err)
	}
	b.templateMenu.Open(b.selectedCol, refForColumn(col), tmpls)
	return b, warnCmd
}

func (a templateMenuActions) update(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	if b.templateMenu.Filtering() {
		switch msg.Code {
		case tea.KeyEsc:
			b.templateMenu.StopFilter()
		case tea.KeyEnter:
			return a.run(templateMenuUse)
		case tea.KeyBackspace:
			b.templateMenu.Backspace()
		default:
			if msg.Text != "" {
				b.templateMenu.AppendFilter(msg.Text)
			} else {
				b.templateMenu.Update(msg)
			}
		}
		return b, nil
	}

	switch {
	case key.Matches(msg, Keys.HelpClose) || msg.String() == "q":
		b.templateMenu.Close()
	case msg.String() == "/":
		b.templateMenu.StartFilter()
	case msg.Code == tea.KeyEnter || msg.String() == "u":
		return a.run(templateMenuUse)
	case msg.String() == "a":
		return a.run(templateMenuAuthor)
	case msg.String() == "e":
		return a.run(templateMenuEdit)
	case msg.String() == "d":
		return a.run(templateMenuRemove)
	default:
		b.templateMenu.Update(msg)
	}
	return b, nil
}

func (a templateMenuActions) run(action templateMenuAction) (tea.Model, tea.Cmd) {
	b := a.board
	entry, ok := b.templateMenu.SelectAction(action)
	if !ok {
		return b, nil
	}
	switch action {
	case templateMenuUse:
		if entry.Kind == templateMenuEntryAuthor {
			return a.run(templateMenuAuthor)
		}
		b.templateMenu.Close()
		return b, b.clipboardActions().openTemplate(templateStartFormMsg{Column: b.templateMenu.column, ColIndex: b.templateMenu.colIndex, Template: entry.Template})
	case templateMenuAuthor:
		b.templateMenu.Close()
		return b, b.templateFlow.OpenAuthor(b.templateMenu.colIndex, b.templateMenu.column, true)
	case templateMenuEdit:
		b.templateMenu.Close()
		return b, b.editor.OpenManagedFile(entry.Template.Name, entry.Template.Path)
	case templateMenuRemove:
		b.templateMenu.Close()
		b.dialog.OpenConfirmDestructive(
			"Remove template?",
			entry.Template.Scope+": "+entry.Template.Name,
			"Remove",
			newStableTemplateRemoveConfirmMsg(b.templateMenu.column, entry.Template),
		)
	}
	return b, nil
}

func (a templateMenuActions) reopenForColumn(col *Column) tea.Cmd {
	_, cmd := a.open(col)
	return cmd
}

func (h boardMutationHandlers) handleManagedFileSave(msg managedFileSaveMsg) (tea.Model, tea.Cmd) {
	b := h.board
	if _, err := os.Stat(msg.Path); err != nil {
		return b, b.notifier.ErrorCause("failed to save "+msg.Label, err)
	}
	if isTemplateFilePath(msg.Path) {
		if err := validateTemplateContent(msg.Path, msg.Content); err != nil {
			return b, b.notifier.ErrorCause("template", err)
		}
	}
	if err := board.ReplaceFileContent(msg.Path, msg.Content); err != nil {
		return b, b.notifier.ErrorCause("failed to save "+msg.Label, err)
	}
	b.editor.confirmSaved()
	return b, b.notifier.Success("saved " + msg.Label)
}

func isTemplateFilePath(path string) bool {
	return filepath.Base(filepath.Dir(path)) == template.Dir
}

func validateTemplateContent(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kbrd-template-*.md")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = template.Parse(tmpPath)
	return err
}

func (h boardMutationHandlers) handleTemplateRemoveConfirm(msg templateRemoveConfirmMsg) (tea.Model, tea.Cmd) {
	b := h.board
	if msg.Path == "" {
		return b, b.notifier.Error("template no longer exists")
	}
	if err := os.Remove(msg.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return b, b.notifier.Error("template no longer exists")
		}
		return b, b.notifier.ErrorCause("failed to remove template", err)
	}
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.Success("removed template " + filepath.Base(msg.Path))
	}
	reopenCmd := b.templateMenuActions().reopenForColumn(col)
	return b, tea.Batch(reopenCmd, b.notifier.Success("removed template "+filepath.Base(msg.Path)))
}
