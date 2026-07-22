package model

import (
	"errors"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"kbrd/events"
)

func (a boardItemActions) edit(colIdx int, col *Column, item *Item) tea.Cmd {
	b := a.board
	b.bus.Publish(events.ItemOpen{
		Item: events.ItemRef{Column: col.Name, Name: item.Name},
		Kind: "edit",
	})
	return b.editor.OpenEdit(colIdx, col.Path, item.Name, item.FullPath)
}

func (a boardItemActions) peek(col *Column, item *Item) tea.Cmd {
	b := a.board
	content, err := col.CopyContent(item.Name)
	if err != nil {
		return b.notifier.ErrorCause("failed to peek", err)
	}
	return b.openPeekForItem(item, string(content))
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
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("open clipboard history", err)
	}
	entry := b.newClipboardEntry(string(content), b.clipboardSource(col, item.Name), map[string]any{
		"bytes": len(content),
		"lines": strings.Count(string(content), "\n") + 1,
	})
	return b.utilityActions().copyToClipboardWithEntry(content, store, entry)
}

func (a boardItemActions) paste(colIdx int, col *Column, item *Item) tea.Cmd {
	return a.board.pasteActions().openMenu(colIdx, col, item)
}

func (a boardItemActions) share(col *Column, item *Item) tea.Cmd {
	b := a.board
	b.bus.Publish(events.ItemOpen{
		Item: events.ItemRef{Column: col.Name, Name: item.Name},
		Kind: "share",
	})
	return func() tea.Msg {
		if err := shareFile(item.FullPath); err != nil {
			return notifyMsg{Message: "failed to share: " + err.Error(), Type: notifyError}
		}
		return notifyMsg{Message: "opened share sheet for " + item.Name, Type: notifyInfo}
	}
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
		return a.paste(colIdx, col, item)
	case 'y':
		return a.share(col, item)
	case 'o':
		return a.openExternal(col, item)
	case '!':
		return a.togglePin(col, item)
	case 'd':
		return a.confirmDelete(colIdx, col, item)
	case 'm':
		return a.board.moveMenuActions().open(itemActionContext{
			Board: a.board, ColIdx: colIdx, Column: col, Item: item,
			Targets: []itemActionTarget{{Ref: refForItem(col, item), Item: *item}}, Source: actionSourceKey,
		})
	case 'M':
		return a.moveNext(colIdx, col, item, false)
	case 'u':
		cmd, _ := b.itemActions().Invoke(actionTimeline, actionSourceKey)
		return cmd
	}
	return b.notifier.Error("unknown command: " + string(action))
}
