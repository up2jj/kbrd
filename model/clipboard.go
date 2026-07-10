package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

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
)

type clipboardReadState struct {
	request  uint64
	kind     clipboardReadKind
	paste    pasteMenuTarget
	template templateStartFormMsg
}

type clipboardFallbackMsg struct{ request uint64 }

type clipboardSystemReadMsg struct {
	request uint64
	content string
}

type boardClipboardActions struct {
	board *Board
}

func (b *Board) clipboardActions() boardClipboardActions {
	return boardClipboardActions{board: b}
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
	case templateStartFormMsg:
		return b, a.openTemplate(msg), true
	default:
		return b, nil, false
	}
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
		return b, b.editor.PasteClipboard(content)
	case clipboardReadTemplate:
		if b.templateFlow.stage != tfClipboard {
			return b, nil
		}
		return b, b.templateFlow.OpenTemplate(state.template.ColIndex, state.template.Column, state.template.Template, content)
	default:
		return b, nil
	}
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
