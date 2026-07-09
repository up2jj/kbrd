package model

import (
	"errors"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (a boardItemActions) copyTargets(col *Column, targets []itemActionTarget) tea.Cmd {
	if len(targets) == 0 {
		return a.board.notifier.Error("no marked items")
	}
	var b strings.Builder
	failures := 0
	for i, target := range targets {
		item := target.Item
		content, err := col.CopyContent(item.Name)
		if err != nil {
			failures++
			continue
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("--- ")
		b.WriteString(item.Name)
		b.WriteString(".md ---\n")
		b.Write(content)
	}
	if b.Len() == 0 {
		return a.board.notifier.Error("failed to copy marked items")
	}
	cmd := a.board.utilityActions().copyToClipboard([]byte(b.String()))
	if failures > 0 {
		return tea.Batch(cmd, a.board.notifier.Error(strconv.Itoa(failures)+" marked item(s) could not be copied"))
	}
	return cmd
}

func (a boardItemActions) moveTargets(srcIdx int, src *Column, targets []itemActionTarget, dstIdx int, selectTarget bool) tea.Cmd {
	b := a.board
	if len(targets) == 0 {
		return b.notifier.Error("no marked items")
	}
	if len(b.columns) == 0 || dstIdx < 0 || dstIdx >= len(b.columns) {
		return b.notifier.Error("target column no longer exists")
	}
	if srcIdx == dstIdx {
		return nil
	}
	dst := b.columns[dstIdx]
	moved := 0
	failures := 0
	lastMoved := ""
	for _, target := range targets {
		col, item, err := b.resolveDelayedItemRef(target.Ref)
		if err != nil || col != src {
			failures++
			continue
		}
		if err := b.moveItem(src, dst, item.Name); err != nil {
			failures++
			continue
		}
		moved++
		lastMoved = item.Name
	}
	if moved > 0 && selectTarget {
		b.selectedCol = dstIdx
		dst.SelectByName(lastMoved)
	}
	switch {
	case moved > 0 && failures > 0:
		return b.notifier.Error("moved " + strconv.Itoa(moved) + ", failed " + strconv.Itoa(failures))
	case moved > 0:
		return b.notifier.Success("moved " + strconv.Itoa(moved) + " item(s) → " + dst.Name)
	default:
		return b.notifier.Error("failed to move marked items")
	}
}

func (a boardItemActions) confirmDeleteTargets(colIdx int, col *Column, targets []itemActionTarget) tea.Cmd {
	if len(targets) == 0 {
		return a.board.notifier.Error("no marked items")
	}
	refs := make([]itemRefStable, len(targets))
	for i, target := range targets {
		refs[i] = target.Ref
	}
	title := "Delete " + strconv.Itoa(len(refs)) + " item(s)?"
	body := col.Name + " — this cannot be undone."
	a.board.dialog.OpenConfirmDestructive(title, body, "Delete", batchDeleteConfirmMsg{ColIndex: colIdx, Targets: refs})
	return nil
}

func (a boardItemActions) handleBatchDelete(msg batchDeleteConfirmMsg) tea.Cmd {
	b := a.board
	deleted := 0
	failures := 0
	var lastErr error
	for _, ref := range msg.Targets {
		col, item, err := b.resolveDelayedItemRef(ref)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		if col.Virtual {
			failures++
			lastErr = errVirtualColumn
			continue
		}
		if err := b.deleteItem(col, item.Name); err != nil {
			failures++
			lastErr = err
			continue
		}
		b.reloadColumnAfterMutation(col)
		deleted++
	}
	switch {
	case deleted > 0 && failures > 0:
		return b.notifier.Error("deleted " + strconv.Itoa(deleted) + ", failed " + strconv.Itoa(failures))
	case deleted > 0:
		return b.notifier.Success("deleted " + strconv.Itoa(deleted) + " item(s)")
	case lastErr != nil:
		if errors.Is(lastErr, os.ErrNotExist) {
			return b.notifier.Error("items no longer exist")
		}
		return b.notifier.ErrorCause("failed to delete", lastErr)
	default:
		return b.notifier.Error("failed to delete marked items")
	}
}

type batchDeleteConfirmMsg struct {
	ColIndex int
	Targets  []itemRefStable
}
