package model

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"kbrd/board"
	"kbrd/browseredit"
	"kbrd/events"
)

type browserEditorTarget struct {
	Ref          itemRefStable
	ExpectedPath string
}

type browserEditorSaveRequestMsg struct{ Request browseredit.SaveRequest }
type browserEditorStoppedMsg struct{}
type browserOpenExistingMsg struct{ SessionID string }
type browserTakeoverMsg struct{ SessionID string }
type browserHandoffResultMsg struct {
	SessionID string
	Err       error
}
type browserTakeoverContinueMsg struct{ SessionID string }
type browserTakeoverCancelMsg struct{ SessionID string }
type browserTakeoverForceMsg struct{ SessionID string }

func (b *Board) ensureBrowserEditor() *browseredit.Manager {
	if b.browserEditor == nil {
		b.browserEditor = browseredit.New()
	}
	if b.browserTargets == nil {
		b.browserTargets = make(map[string]browserEditorTarget)
	}
	return b.browserEditor
}

func (b *Board) closeBrowserEditor(reason string) {
	if b.browserEditor != nil {
		for id := range b.browserTargets {
			b.browserEditor.Invalidate(id, reason)
		}
		_ = b.browserEditor.Close()
	}
	b.browserEditor = nil
	b.browserTargets = nil
	b.browserSaveWaitArmed = false
}

func (b *Board) openBrowserEditor(col *Column, item *Item) tea.Cmd {
	if col == nil || item == nil || col.Virtual || item.Virtual || item.FullPath == "" {
		return b.notifier.Error("browser editing requires a filesystem card")
	}
	if b.tuiOwnsPath(item.FullPath) {
		return b.notifier.Error("This card is already open in the TUI editor.")
	}
	mgr := b.ensureBrowserEditor()
	opened, err := mgr.Open(browseredit.Card{
		Path: item.FullPath, BoardName: b.cfg.BoardName,
		ColumnName: col.Name, CardName: item.Name,
		LinkTargets: b.browserLinkTargets(item.FullPath),
	})
	if err != nil {
		return b.notifier.ErrorCause("failed to open browser editor", err)
	}
	canonical, err := filepath.EvalSymlinks(item.FullPath)
	if err != nil {
		return b.notifier.ErrorCause("failed to resolve card path", err)
	}
	canonical, _ = filepath.Abs(canonical)
	b.browserTargets[opened.ID] = browserEditorTarget{Ref: refForItem(col, item), ExpectedPath: filepath.Clean(canonical)}
	b.bus.Publish(events.ItemOpen{Item: events.ItemRef{Column: col.Name, Name: item.Name}, Kind: "browser"})
	openCmd := func() tea.Msg {
		if err := openFile(opened.URL); err != nil {
			return notifyMsg{Message: "failed to open browser: " + err.Error(), Type: notifyError}
		}
		return notifyMsg{Message: "opened " + item.Name + " in browser", Type: notifyInfo}
	}
	if b.browserSaveWaitArmed {
		return openCmd
	}
	b.browserSaveWaitArmed = true
	return tea.Batch(openCmd, waitBrowserEditorSave(mgr))
}

func (b *Board) browserLinkTargets(currentPath string) []browseredit.LinkTarget {
	targets := make([]browseredit.LinkTarget, 0)
	for _, col := range b.allFilesystemColumns() {
		for i := range col.Items {
			item := &col.Items[i]
			if item.Virtual || samePath(item.FullPath, currentPath) || !wikiLinkSafeName(item.Name) {
				continue
			}
			targets = append(targets, browseredit.LinkTarget{Name: item.Name, Column: col.Name})
		}
	}
	slices.SortFunc(targets, func(a, z browseredit.LinkTarget) int {
		if order := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(z.Name)); order != 0 {
			return order
		}
		return cmp.Compare(strings.ToLower(a.Column), strings.ToLower(z.Column))
	})
	return targets
}

func wikiLinkSafeName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "[]|\r\n")
}

func waitBrowserEditorSave(mgr *browseredit.Manager) tea.Cmd {
	return func() tea.Msg {
		request, ok := <-mgr.SaveRequests()
		if !ok {
			return browserEditorStoppedMsg{}
		}
		return browserEditorSaveRequestMsg{Request: request}
	}
}

func (b *Board) handleBrowserEditorSave(msg browserEditorSaveRequestMsg) (tea.Model, tea.Cmd) {
	request := msg.Request
	result := browseredit.SaveResult{}
	target, ok := b.browserTargets[request.SessionID]
	if !ok {
		result.Gone = true
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	col, item, err := b.resolveBrowserTarget(target)
	if err != nil {
		result.Gone = true
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	raw, err := os.ReadFile(item.FullPath)
	if err != nil {
		result.Gone = errors.Is(err, os.ErrNotExist)
		result.Err = err
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	current := browseredit.ParseDocument(string(raw))
	if current.Revision != request.BaseRevision {
		result.Document, result.Conflict = current, true
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	merged, err := browseredit.MergeBody(string(raw), request.Body)
	if err != nil {
		result.Err = err
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	if err := b.mutationHandlers().writeResolvedExistingItem(col, item, "browser", func(item *Item) error {
		return board.ReplaceFileContent(item.FullPath, merged)
	}); err != nil {
		result.Err = err
		request.Reply <- result
		return b, waitBrowserEditorSave(b.browserEditor)
	}
	updated, err := os.ReadFile(target.ExpectedPath)
	if err != nil {
		result.Err = err
	} else {
		result.Document = browseredit.ParseDocument(string(updated))
	}
	request.Reply <- result
	return b, tea.Batch(b.notifier.Success("saved "+item.Name+" in browser"), waitBrowserEditorSave(b.browserEditor))
}

func (b *Board) resolveBrowserTarget(target browserEditorTarget) (*Column, *Item, error) {
	col, err := b.resolveDelayedColumnRef(target.Ref.Column)
	if err != nil || col.Virtual || target.Ref.ItemPath == "" {
		return nil, nil, fmt.Errorf("browser card no longer exists")
	}
	for i := range col.Items {
		item := &col.Items[i]
		if !samePath(item.FullPath, target.Ref.ItemPath) {
			continue
		}
		canonical, err := filepath.EvalSymlinks(item.FullPath)
		if err != nil {
			return nil, nil, err
		}
		canonical, _ = filepath.Abs(canonical)
		if filepath.Clean(canonical) != target.ExpectedPath {
			return nil, nil, fmt.Errorf("browser card moved to an unexpected path")
		}
		return col, item, nil
	}
	return nil, nil, fmt.Errorf("browser card no longer exists")
}

func (b *Board) tuiOwnsPath(path string) bool {
	if b.editor != nil && b.editor.state != editorNone && b.editor.ItemPath != "" && samePath(b.editor.ItemPath, path) {
		return true
	}
	return b.frontmatterEdit.Active() && b.frontmatterEdit.target.ItemPath != "" && samePath(b.frontmatterEdit.target.ItemPath, path)
}

// acquireCardEditor is the Board-owned entry point for interactive existing-card
// buffers. It enforces mutual exclusion with a browser writer.
func (b *Board) acquireCardEditor(colIdx int, col *Column, item *Item, open func() tea.Cmd) tea.Cmd {
	if b.browserEditor == nil || item == nil || item.FullPath == "" {
		return open()
	}
	state, ok := b.browserEditor.SessionForPath(item.FullPath)
	if !ok {
		return open()
	}
	if !state.Active {
		if err := b.browserEditor.PrepareRecovery(state.ID); err != nil {
			return b.notifier.ErrorCause("failed to prepare browser recovery", err)
		}
		b.browserEditor.Invalidate(state.ID, "opened in TUI")
		delete(b.browserTargets, state.ID)
		return b.openFullCardEditor(colIdx, col, item)
	}
	b.dialog.Open(DialogOptions{
		Title: "This card is being edited in the browser.",
		Buttons: []DialogButton{
			{Label: "Open browser", Kind: ButtonPrimary, Msg: browserOpenExistingMsg{SessionID: state.ID}},
			{Label: "Take over in TUI", Kind: ButtonDanger, Msg: browserTakeoverMsg{SessionID: state.ID}},
			{Label: "Cancel"},
		},
		DefaultIndex: 0,
	})
	return nil
}

func (b *Board) openFullCardEditor(colIdx int, col *Column, item *Item) tea.Cmd {
	b.bus.Publish(events.ItemOpen{Item: events.ItemRef{Column: col.Name, Name: item.Name}, Kind: "edit"})
	return b.editor.OpenEdit(colIdx, col.Path, item.Name, item.FullPath)
}

func (b *Board) handleBrowserOpenExisting(msg browserOpenExistingMsg) (tea.Model, tea.Cmd) {
	url, ok := b.browserEditor.URL(msg.SessionID)
	if !ok {
		return b, b.notifier.Error("browser editor session is no longer available")
	}
	return b, func() tea.Msg {
		if err := openFile(url); err != nil {
			return notifyMsg{Message: "failed to open browser: " + err.Error(), Type: notifyError}
		}
		return notifyMsg{Message: "opened browser editor", Type: notifyInfo}
	}
}

func (b *Board) handleBrowserTakeover(msg browserTakeoverMsg) (tea.Model, tea.Cmd) {
	mgr := b.browserEditor
	return b, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := mgr.RequestHandoff(ctx, msg.SessionID)
		if err == nil {
			err = mgr.PrepareRecovery(msg.SessionID)
		}
		return browserHandoffResultMsg{SessionID: msg.SessionID, Err: err}
	}
}

func (b *Board) handleBrowserHandoffResult(msg browserHandoffResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		b.dialog.Open(DialogOptions{
			Title:        "Browser handoff did not complete.",
			Body:         "Continue with the last locally recovered draft? The browser's most recent unflushed keystrokes may be unavailable.",
			Buttons:      []DialogButton{{Label: "Continue", Kind: ButtonDanger, Msg: browserTakeoverContinueMsg{SessionID: msg.SessionID}}, {Label: "Cancel", Msg: browserTakeoverCancelMsg{SessionID: msg.SessionID}}},
			DefaultIndex: 1,
		})
		return b, nil
	}
	return b.finishBrowserTakeover(msg.SessionID)
}

func (b *Board) handleBrowserTakeoverContinue(msg browserTakeoverContinueMsg) (tea.Model, tea.Cmd) {
	if err := b.browserEditor.PrepareRecovery(msg.SessionID); err != nil {
		b.dialog.Open(DialogOptions{
			Title:        "Recovery metadata could not be rebased.",
			Body:         "Continue with the last durable sidecar unchanged? It may contain an older frontmatter prefix.",
			Buttons:      []DialogButton{{Label: "Continue", Kind: ButtonDanger, Msg: browserTakeoverForceMsg{SessionID: msg.SessionID}}, {Label: "Cancel", Msg: browserTakeoverCancelMsg{SessionID: msg.SessionID}}},
			DefaultIndex: 1,
		})
		return b, nil
	}
	return b.finishBrowserTakeover(msg.SessionID)
}

func (b *Board) handleBrowserTakeoverCancel(msg browserTakeoverCancelMsg) (tea.Model, tea.Cmd) {
	if b.browserEditor != nil {
		b.browserEditor.CancelHandoff(msg.SessionID)
	}
	return b, nil
}

func (b *Board) handleBrowserTakeoverForce(msg browserTakeoverForceMsg) (tea.Model, tea.Cmd) {
	return b.finishBrowserTakeover(msg.SessionID)
}

func (b *Board) finishBrowserTakeover(id string) (tea.Model, tea.Cmd) {
	target, ok := b.browserTargets[id]
	if !ok {
		return b, b.notifier.Error("browser editor target is no longer available")
	}
	col, item, err := b.resolveBrowserTarget(target)
	if err != nil {
		return b, b.notifier.ErrorCause("cannot take over browser editor", err)
	}
	b.browserEditor.Invalidate(id, "ownership moved to TUI")
	delete(b.browserTargets, id)
	return b, b.openFullCardEditor(b.indexOfColumn(col), col, item)
}
