package model

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"kbrd/clipboardring"
	"kbrd/template"
)

const clipboardFallbackDelay = 150 * time.Millisecond

// clipboardReadKind identifies the UI operation that requested a clipboard
// read. Bubble Tea delivers OSC52 responses without a request ID, so the Board
// tracks one request and routes either its terminal response or its system
// clipboard fallback through this state.
type clipboardReadKind int

const (
	clipboardReadNone clipboardReadKind = iota
	clipboardReadPasteMenu
	clipboardReadEditor
	clipboardReadTemplate
	clipboardReadRingImport
)

type clipboardReadState struct {
	request  uint64
	kind     clipboardReadKind
	paste    pasteMenuTarget
	template templateStartFormMsg
	ring     pasteMenuTarget
}

type clipboardFallbackMsg struct{ request uint64 }

type clipboardSystemReadMsg struct {
	request uint64
	content string
}

type editorYankMsg struct {
	column     columnRef
	fileName   string
	content    string
	scratchpad bool
}

type boardClipboardActions struct {
	board *Board
}

func (b *Board) clipboardActions() boardClipboardActions {
	return boardClipboardActions{board: b}
}

func (b *Board) clipboardStore() (*clipboardring.Store, error) {
	if b.clipboardRing != nil {
		return b.clipboardRing, nil
	}
	store, err := clipboardring.Open("")
	if err != nil {
		return nil, err
	}
	b.clipboardRing = store
	return store, nil
}

func (b *Board) clipboardRingEntries() []clipboardring.Entry {
	store, err := b.clipboardStore()
	if err != nil {
		return nil
	}
	return store.Entries()
}

func (b *Board) newClipboardEntry(text string, source clipboardring.Source, metadata map[string]any) clipboardring.Entry {
	return clipboardring.Entry{
		ID:       fmt.Sprintf("%d", time.Now().UnixNano()),
		Time:     time.Now(),
		Kind:     clipboardring.DetectKind(text),
		Text:     text,
		Source:   source,
		Metadata: metadata,
	}
}

func (b *Board) clipboardSource(col *Column, card string) clipboardring.Source {
	boardName := filepath.Base(b.cfg.Path)
	if b.cfg.BoardName != "" {
		boardName = b.cfg.BoardName
	}
	return clipboardring.Source{Board: boardName, Column: col.Name, Card: card}
}

func (a boardClipboardActions) request(state clipboardReadState) tea.Cmd {
	b := a.board
	b.clipboardReadSeq++
	state.request = b.clipboardReadSeq
	b.clipboardRead = state
	return tea.Batch(
		func() tea.Msg { return tea.ReadClipboard() },
		tea.Tick(clipboardFallbackDelay, func(time.Time) tea.Msg {
			return clipboardFallbackMsg{request: state.request}
		}),
	)
}

func (a boardClipboardActions) readPasteMenu(target pasteMenuTarget) tea.Cmd {
	return a.request(clipboardReadState{kind: clipboardReadPasteMenu, paste: target})
}

func (a boardClipboardActions) readEditor() tea.Cmd {
	return a.request(clipboardReadState{kind: clipboardReadEditor})
}

func (a boardClipboardActions) readRingImport(target pasteMenuTarget) tea.Cmd {
	return a.request(clipboardReadState{kind: clipboardReadRingImport, ring: target})
}

func (a boardClipboardActions) cancelTemplateRead() {
	if a.board.clipboardRead.kind == clipboardReadTemplate {
		a.board.clipboardRead = clipboardReadState{}
	}
}

func (a boardClipboardActions) openTemplate(msg templateStartFormMsg) tea.Cmd {
	if !templateUsesClipboard(msg.Template) {
		return a.board.templateFlow.OpenTemplate(msg.ColIndex, msg.Column, msg.Template, "")
	}
	a.board.templateFlow.WaitForClipboard(msg.ColIndex, msg.Column, msg.Template)
	return a.request(clipboardReadState{kind: clipboardReadTemplate, template: msg})
}

// handle owns all clipboard-specific messages so Board.updateInner only has to
// delegate once instead of growing a case for every clipboard consumer.
func (a boardClipboardActions) handle(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	b := a.board
	switch msg := msg.(type) {
	case tea.ClipboardMsg:
		model, cmd := a.handleContent(msg.Content)
		return model, cmd, true
	case clipboardFallbackMsg:
		if msg.request != b.clipboardRead.request || b.clipboardRead.kind == clipboardReadNone {
			return b, nil, true
		}
		return b, a.readSystemClipboard(msg.request), true
	case clipboardSystemReadMsg:
		if msg.request != b.clipboardRead.request || b.clipboardRead.kind == clipboardReadNone {
			return b, nil, true
		}
		model, cmd := a.handleContent(msg.content)
		return model, cmd, true
	case editorClipboardReadMsg:
		return b, a.readEditor(), true
	case editorYankMsg:
		return b, a.recordEditorYank(msg), true
	case templateStartFormMsg:
		return b, a.openTemplate(msg), true
	default:
		return b, nil, false
	}
}

func (a boardClipboardActions) recordEditorYank(msg editorYankMsg) tea.Cmd {
	b := a.board
	if msg.content == "" {
		return nil
	}
	var source clipboardring.Source
	if msg.scratchpad {
		source = b.scratchpadActions().clipboardSource()
	} else {
		col, err := b.resolveDelayedColumnRef(msg.column)
		if err != nil {
			return nil
		}
		source = b.clipboardSource(col, msg.fileName)
	}
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("open clipboard history", err)
	}
	metadata := map[string]any{
		"bytes":       len(msg.content),
		"lines":       strings.Count(msg.content, "\n") + 1,
		"editor_yank": true,
	}
	if msg.scratchpad {
		metadata["scratchpad"] = true
	}
	entry := b.newClipboardEntry(msg.content, source, metadata)
	if err := store.Add(entry); err != nil {
		return b.notifier.ErrorCause("save clipboard history", err)
	}
	return nil
}

func (a boardClipboardActions) readSystemClipboard(request uint64) tea.Cmd {
	return func() tea.Msg {
		content, _ := clipboard.ReadAll()
		return clipboardSystemReadMsg{request: request, content: content}
	}
}

func (a boardClipboardActions) handleContent(content string) (tea.Model, tea.Cmd) {
	b := a.board
	state := b.clipboardRead
	b.clipboardRead = clipboardReadState{}

	switch state.kind {
	case clipboardReadPasteMenu:
		return b.pasteActions().openMenuWithText(state.paste, content)
	case clipboardReadEditor:
		if b.editor.IsScratchpad() {
			return b, b.scratchpadActions().insertClipboard(content)
		}
		return b, b.editor.PasteClipboard(content)
	case clipboardReadTemplate:
		if b.templateFlow.stage != tfClipboard {
			return b, nil
		}
		return b, b.templateFlow.OpenTemplate(state.template.ColIndex, state.template.Column, state.template.Template, content)
	case clipboardReadRingImport:
		return b, a.importRingContent(content, state.ring)
	default:
		return b, nil
	}
}

func (a boardClipboardActions) importRingContent(content string, target pasteMenuTarget) tea.Cmd {
	b := a.board
	if strings.TrimSpace(content) == "" {
		return b.notifier.Error("system clipboard is empty or unavailable")
	}
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("open clipboard history", err)
	}
	entry := b.newClipboardEntry(content, clipboardring.Source{}, map[string]any{
		"bytes":            len(content),
		"lines":            strings.Count(content, "\n") + 1,
		"system_clipboard": true,
	})
	if err := store.Add(entry); err != nil {
		return b.notifier.ErrorCause("save clipboard history", err)
	}
	if b.clipboardMenu.Active() {
		b.clipboardMenu.Open(store.Entries(), target)
	}
	return b.notifier.Success("imported system clipboard")
}

func templateUsesClipboard(tmpl template.Template) bool {
	for _, step := range tmpl.Steps {
		for _, field := range step.Fields {
			if field.Prefill == template.PrefillClipboard {
				return true
			}
		}
	}
	return false
}
