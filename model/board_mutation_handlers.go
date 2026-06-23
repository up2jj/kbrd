package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/board"
	"kbrd/events"
	"kbrd/template"
)

type boardMutationHandlers struct {
	board *Board
}

func (b *Board) mutationHandlers() boardMutationHandlers {
	return boardMutationHandlers{board: b}
}

func (h boardMutationHandlers) handleSave(msg editorSaveMsg) (tea.Model, tea.Cmd) {
	// board.ReplaceFileContent is existing-only: a card deleted while the
	// editor was open errors instead of being silently resurrected.
	return h.writeExistingItem(msg.Target, msg.FileName, "save", "failed to save", func(item *Item) error {
		return board.ReplaceFileContent(item.FullPath, msg.Content)
	}, func(name string) string {
		return "saved " + name
	})
}

func (h boardMutationHandlers) handleAppend(msg editorAppendMsg) (tea.Model, tea.Cmd) {
	return h.writeExistingItem(msg.Target, msg.FileName, "append", "failed to append", func(item *Item) error {
		return board.AppendLine(item.FullPath, msg.Text)
	}, func(name string) string {
		return "appended to " + name
	})
}

func (h boardMutationHandlers) handlePrepend(msg editorPrependMsg) (tea.Model, tea.Cmd) {
	return h.writeExistingItem(msg.Target, msg.FileName, "prepend", "failed to prepend", func(item *Item) error {
		return board.PrependLine(item.FullPath, msg.Text)
	}, func(name string) string {
		return "prepended to " + name
	})
}

func (h boardMutationHandlers) handleJournal(msg editorJournalMsg) (tea.Model, tea.Cmd) {
	b := h.board
	at, body := b.journalStamp(msg.Text)
	return h.writeExistingItem(msg.Target, msg.FileName, "journal", "failed to journal: ", func(item *Item) error {
		return board.JournalLine(item.FullPath, at, body)
	}, func(name string) string {
		return "journal entry added to " + name
	})
}

func (h boardMutationHandlers) writeExistingItem(target itemRefStable, fallbackName, kind, errorPrefix string, write func(*Item) error, success func(string) string) (tea.Model, tea.Cmd) {
	b := h.board
	if target.FileName == "" {
		target.FileName = fallbackName
	}
	col, item, err := b.resolveDelayedItemRef(target)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	if err := write(item); err != nil {
		return b, b.notifier.ErrorCause(errorPrefix, err)
	}
	b.editor.confirmSaved()
	b.reloadColumnAfterMutation(col)
	col.SelectByName(item.Name)
	b.bus.Publish(events.ItemSaved{Item: events.ItemRef{Column: col.Name, Name: item.Name}, Kind: kind})
	return b, b.notifier.Success(success(item.Name))
}

// openTemplateFlow starts the unified create overlay for col: an empty-card
// action plus the column's .kbrd_templates merged with board-level templates.
func (h boardMutationHandlers) openTemplateFlow(col *Column) (tea.Model, tea.Cmd) {
	b := h.board
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
	return b, tea.Batch(warnCmd, b.templateFlow.Open(b.selectedCol, refForColumn(col), tmpls))
}

func (h boardMutationHandlers) handleCreateEmptyItem(msg createEmptyItemMsg) (tea.Model, tea.Cmd) {
	b := h.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	return b, b.editor.OpenNew(msg.ColIndex, col.Name, col.Path)
}

// handleTemplateSubmit renders the completed template form and creates the
// new card, mirroring handleNew's error reporting.
func (h boardMutationHandlers) handleTemplateSubmit(msg templateSubmitMsg) (tea.Model, tea.Cmd) {
	b := h.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	vctx := board.VarContext{
		BoardPath:  b.cfg.Path,
		BoardName:  b.cfg.BoardName,
		ColumnPath: col.Path,
		ColumnName: col.Name,
	}
	name, body, err := template.Instantiate(msg.Template, vctx, msg.Values)
	if err != nil {
		return b, b.notifier.ErrorCause("template", err)
	}
	// Resolve {{shell}} markers: rewrite to inert notes when exec is disabled,
	// or spawn a background worker per marker that fills it in. cardPath is the
	// path the card is about to be written to.
	cardPath := filepath.Join(col.Path, name+".md")
	body, shellCmd := b.templateExec.dispatch(cardPath, body, b.cfg.Path, b.cfg.Template)
	if _, err := b.createItemContent(col, name, body); err != nil {
		if errors.Is(err, os.ErrExist) {
			return b, b.notifier.Error("file already exists: " + name + ".md")
		}
		return b, b.notifier.ErrorCause("failed to create", err)
	}
	col.SelectByName(name)
	return b, tea.Batch(shellCmd, b.notifier.Success("created "+name+".md"))
}

func (h boardMutationHandlers) handleTemplateAuthorSubmit(msg templateAuthorSubmitMsg) (tea.Model, tea.Cmd) {
	b := h.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	created, err := createColumnTemplate(col.Path, msg.Values)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	var reopenCmd tea.Cmd
	if msg.ReopenMenu {
		reopenCmd = b.templateMenuActions().reopenForColumn(col)
	}
	return b, tea.Batch(reopenCmd, b.notifier.Success("created column template "+created.FileName))
}

func (h boardMutationHandlers) handleNew(msg editorNewMsg) (tea.Model, tea.Cmd) {
	b := h.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	if msg.FileName == "" {
		return b, b.notifier.Error("filename cannot be empty")
	}
	if _, err := b.createItem(col, msg.FileName); err != nil {
		return b, b.notifier.ErrorCause("failed to create", err)
	}
	col.SelectByName(msg.FileName)
	return b, b.notifier.Success("created " + msg.FileName + ".md")
}

func validateRenameName(name string) error {
	_, err := board.SanitizeFolder(name)
	return err
}

func (h boardMutationHandlers) handleRenameItemRequest(msg renameItemRequestMsg) (tea.Model, tea.Cmd) {
	b := h.board
	newName := strings.TrimSpace(msg.NewName)
	if err := validateRenameName(newName); err != nil {
		return b, b.notifier.ErrorCause("invalid name", err)
	}
	if newName == msg.OldName {
		return b, nil
	}
	targetRef := msg.Target
	if targetRef.FileName == "" {
		targetRef.FileName = msg.OldName
	}
	col, item, err := b.resolveDelayedItemRef(targetRef)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	target := filepath.Join(col.Path, newName+".md")
	if _, err := os.Stat(target); err == nil {
		return b, b.notifier.Error("file already exists: " + newName + ".md")
	}
	b.dialog.OpenConfirm("Rename item?", item.Name+".md → "+newName+".md", newStableRenameItemConfirmMsg(refForItem(col, item), msg.ColIndex, item.Name, newName))
	return b, nil
}

func (h boardMutationHandlers) handleRenameColumnRequest(msg renameColumnRequestMsg) (tea.Model, tea.Cmd) {
	b := h.board
	newName := strings.TrimSpace(msg.NewName)
	if err := validateRenameName(newName); err != nil {
		return b, b.notifier.ErrorCause("invalid name", err)
	}
	if newName == msg.OldName {
		return b, nil
	}
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	parent := filepath.Dir(col.Path)
	target := filepath.Join(parent, newName)
	if _, err := os.Stat(target); err == nil {
		return b, b.notifier.Error("folder already exists: " + newName)
	}
	b.dialog.OpenConfirm("Rename column?", col.Name+" → "+newName, newStableRenameColumnConfirmMsg(refForColumn(col), msg.ColIndex, col.Name, newName))
	return b, nil
}

func (h boardMutationHandlers) handleRenameItemConfirm(msg renameItemConfirmMsg) (tea.Model, tea.Cmd) {
	b := h.board
	target := msg.Target
	if target.FileName == "" {
		target.FileName = msg.OldName
	}
	col, item, err := b.resolveDelayedItemRef(target)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	if err := b.renameItem(col, item.Name, msg.NewName); err != nil {
		return b, b.notifier.ErrorCause("failed to rename", err)
	}
	col.SelectByName(msg.NewName)
	return b, b.notifier.Success("renamed " + item.Name + " → " + msg.NewName)
}

func (h boardMutationHandlers) handleRenameColumnConfirm(msg renameColumnConfirmMsg) (tea.Model, tea.Cmd) {
	b := h.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	if err := col.Rename(msg.NewName); err != nil {
		return b, b.notifier.ErrorCause("failed to rename", err)
	}
	return b, b.notifier.Success("renamed column " + col.Name + " → " + msg.NewName)
}

func (h boardMutationHandlers) handleDelete(msg deleteConfirmMsg) (tea.Model, tea.Cmd) {
	b := h.board
	target := msg.Target
	if target.FileName == "" {
		target.FileName = msg.FileName
	}
	col, item, err := b.resolveDelayedItemRef(target)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	if err := b.deleteItem(col, item.Name); err != nil {
		return b, b.notifier.ErrorCause("failed to delete", err)
	}
	b.reloadColumnAfterMutation(col)
	return b, b.notifier.Success("deleted " + item.Name)
}
