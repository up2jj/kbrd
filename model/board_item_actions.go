package model

import (
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/events"
)

type boardItemActions struct {
	board *Board
}

func (b *Board) itemActions() boardItemActions {
	return boardItemActions{board: b}
}

func (a boardItemActions) edit(colIdx int, col *Column, item *Item) tea.Cmd {
	b := a.board
	b.bus.Publish(events.ItemOpen{
		Item: events.ItemRef{Column: col.Name, Name: item.Name},
		Kind: "edit",
	})
	return b.editor.OpenEdit(colIdx, col.Path, item.Name, item.FullPath)
}

func (a boardItemActions) append(colIdx int, col *Column, item *Item) tea.Cmd {
	return a.board.editor.OpenAppend(colIdx, col.Path, item.FullPath, item.Name)
}

func (a boardItemActions) prepend(colIdx int, col *Column, item *Item) tea.Cmd {
	return a.board.editor.OpenPrepend(colIdx, col.Path, item.FullPath, item.Name)
}

func (a boardItemActions) journal(colIdx int, col *Column, item *Item) tea.Cmd {
	return a.board.editor.OpenJournal(colIdx, col.Path, item.FullPath, item.Name)
}

func (a boardItemActions) copy(col *Column, item *Item) tea.Cmd {
	b := a.board
	content, err := col.CopyContent(item.Name)
	if err != nil {
		return b.notifier.ErrorCause("failed to copy", err)
	}
	return b.utilityActions().copyToClipboard([]byte(content))
}

func (a boardItemActions) paste(colIdx int, item *Item) tea.Cmd {
	return a.board.pasteActions().openMenu(colIdx, item.Name)
}

func (a boardItemActions) openExternal(col *Column, item *Item) tea.Cmd {
	b := a.board
	if err := col.OpenFile(item.Name); err != nil {
		return b.notifier.ErrorCause("failed to open", err)
	}
	b.bus.Publish(events.ItemOpen{
		Item: events.ItemRef{Column: col.Name, Name: item.Name},
		Kind: "external",
	})
	return b.notifier.Success("opened " + item.Name)
}

func (a boardItemActions) togglePin(col *Column, item *Item) tea.Cmd {
	b := a.board
	wasPinned := item.Pinned
	if err := col.PinItem(item.Name); err != nil {
		return b.notifier.ErrorCause("failed to pin", err)
	}
	b.applyColumnTransform(col)
	pinState := "pinned"
	if wasPinned {
		pinState = "unpinned"
	}
	return b.notifier.Success(item.Name + " " + pinState)
}

func (a boardItemActions) confirmDelete(colIdx int, col *Column, item *Item) tea.Cmd {
	a.board.dialog.OpenConfirmDestructive("Delete item?", item.Name+".md", "Yes", newStableDeleteConfirmMsg(refForItem(col, item), colIdx, item.Name))
	return nil
}

func (a boardItemActions) moveNext(colIdx int, col *Column, item *Item, selectTarget bool) tea.Cmd {
	b := a.board
	nextCol := (colIdx + 1) % len(b.columns)
	toName := b.columns[nextCol].Name
	if err := b.moveItem(col, b.columns[nextCol], item.Name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return b.notifier.Error("file already exists in target: " + item.Name + ".md")
		}
		return b.notifier.ErrorCause("failed to move", err)
	}
	if selectTarget {
		b.selectedCol = nextCol
		b.columns[nextCol].SelectByName(item.Name)
		return nil
	}
	return b.notifier.Success("moved " + item.Name + " → " + toName)
}

func (a boardItemActions) moveFirst(colIdx int, col *Column, item *Item) tea.Cmd {
	b := a.board
	if len(b.columns) == 0 {
		return b.notifier.Error("no folders available")
	}
	if colIdx == 0 {
		return nil
	}
	if err := b.moveItem(col, b.columns[0], item.Name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return b.notifier.Error("file already exists in target: " + item.Name + ".md")
		}
		return b.notifier.ErrorCause("failed to move", err)
	}
	b.selectedCol = 0
	b.columns[0].SelectByName(item.Name)
	return nil
}

func (a boardItemActions) dispatch(action byte, ref itemRefStable) tea.Cmd {
	b := a.board
	col, item, err := b.resolveDelayedItemRef(ref)
	if err != nil {
		return b.notifier.ErrorCause("", err)
	}
	colIdx := b.indexOfColumn(col)
	if colIdx < 0 {
		return b.notifier.Error("column no longer exists")
	}
	if col.Virtual {
		return b.notifier.Error("virtual columns have no built-in item actions — use x")
	}

	switch action {
	case 'e':
		return a.edit(colIdx, col, item)
	case 'a':
		return a.append(colIdx, col, item)
	case 'p':
		return a.prepend(colIdx, col, item)
	case 'J':
		return a.journal(colIdx, col, item)
	case 'c':
		return a.copy(col, item)
	case 'V', 'v':
		return a.paste(colIdx, item)
	case 'o':
		return a.openExternal(col, item)
	case '!':
		return a.togglePin(col, item)
	case 'd':
		return a.confirmDelete(colIdx, col, item)
	case 'm':
		return a.moveNext(colIdx, col, item, false)
	}
	return b.notifier.Error("unknown command: " + string(action))
}
