package model

import tea "charm.land/bubbletea/v2"

// modalLayer is one modal interaction policy. The ordered registry below is
// the single source for overlay paint order, key ownership, and mouse capture.
// Individual screens still own their domain-specific state and commands.
type modalLayer struct {
	active func() bool
	view   func(width, height, frameHeight int) string
	key    func(tea.KeyPressMsg) (tea.Model, tea.Cmd)
	mouse  func(tea.MouseMsg) tea.Cmd
}

func (b *Board) modalLayers() []modalLayer {
	return []modalLayer{
		{
			active: b.helpMenu.Active,
			view:   func(w, h, _ int) string { return b.helpMenu.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.helpActions().update(msg) },
			mouse:  func(msg tea.MouseMsg) tea.Cmd { b.helpMenu.HandleMouse(msg); return nil },
		},
		{
			active: b.harpoon.Active,
			view:   func(w, h, _ int) string { return b.harpoon.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.harpoonActions().update(msg) },
		},
		{
			active: func() bool { return b.configMenuOpen },
			view:   func(_, _, _ int) string { return RenderConfigCommandsOverlay(configCommandEntries()) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.inputRouter().handleConfigMenu(msg) },
		},
		{
			active: func() bool { return b.dialog.active },
			view:   func(_, _, _ int) string { return b.dialog.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.dialog.Update(msg) },
		},
		{
			active: b.conflictReview.Active,
			view:   func(w, h, _ int) string { return b.conflictReview.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.conflictReview.Update(msg) },
		},
		{
			active: b.customCmds.Active,
			view:   func(w, h, _ int) string { return b.customCmds.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.customCmds.Update(msg) },
		},
		{
			active: b.clipboardMenu.Active,
			view:   func(w, h, _ int) string { return b.clipboardMenu.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.clipboardActions().updateBrowser(msg) },
		},
		{
			active: b.pasteMenu.Active,
			view:   func(w, h, _ int) string { return b.pasteMenu.View(w, h) },
			key: func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
				cmd := b.pasteMenu.Update(msg)
				if !b.pasteMenu.Active() {
					cancelled := b.pasteMenu.TakeCancelled()
					if cancelled && b.clipboardReturn {
						b.clipboardReturn = false
						b.clipboardMenu.Open(b.clipboardRingEntries(), b.clipboardTarget)
					} else if !cancelled {
						b.clipboardReturn = false
					}
				}
				return b, cmd
			},
		},
		{
			active: b.scriptUI.Active,
			view:   func(_, _, _ int) string { return b.scriptUI.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.scriptUI.Update(msg) },
			mouse:  func(msg tea.MouseMsg) tea.Cmd { return b.scriptUI.Update(msg) },
		},
		{
			active: func() bool { return b.editor.state != editorNone },
			view:   func(_, _, frameH int) string { return b.editor.viewInFrame(frameH) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.inputRouter().handleEditor(msg) },
			mouse:  func(msg tea.MouseMsg) tea.Cmd { b.editor.HandleMouse(msg); return nil },
		},
		{
			active: b.timeline.Active,
			view:   func(w, h, _ int) string { return b.timeline.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.timeline.Update(msg) },
		},
		{
			active: b.peek.Active,
			view:   func(w, h, _ int) string { return b.peek.View(w, h) },
			key: func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
				if cmd, handled := b.inputRouter().handlePeekAction(msg); handled {
					return b, cmd
				}
				b.peek.Update(msg)
				return b, nil
			},
			mouse: func(msg tea.MouseMsg) tea.Cmd { b.peek.HandleMouse(msg); return nil },
		},
		{
			active: b.switcher.Active,
			view:   func(_, _, _ int) string { return b.switcher.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.switcher.Update(msg) },
		},
		{
			active: b.layerSwitcher.Active,
			view:   func(w, h, _ int) string { return b.layerSwitcher.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.layerSwitcher.Update(msg) },
		},
		{
			active: b.search.Active,
			view:   func(w, h, _ int) string { return b.search.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.search.HandleKey(msg) },
		},
		{
			active: b.templateMenu.Active,
			view:   func(w, h, _ int) string { return b.templateMenu.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.templateMenuActions().update(msg) },
		},
		{
			active: b.presetMenu.Active,
			view:   func(w, h, _ int) string { return b.presetMenu.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.frontmatterPresetActions().update(msg) },
		},
		{
			active: b.moveMenu.Active,
			view:   func(w, h, _ int) string { return b.moveMenu.View(w, h) },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.moveMenuActions().update(msg) },
		},
		{
			active: b.templateFlow.Active,
			view:   func(_, _, _ int) string { return b.templateFlow.View() },
			key: func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
				cmd := b.templateFlow.Update(msg)
				if !b.templateFlow.Active() {
					b.clipboardActions().cancelTemplateRead()
				}
				return b, cmd
			},
		},
		{
			active: b.frontmatterEdit.Active,
			view:   func(_, _, _ int) string { return b.frontmatterEdit.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.frontmatterEdit.Update(msg) },
		},
		{
			active: b.git.Active,
			view:   func(_, _, _ int) string { return b.git.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.git.HandleKey(msg) },
			mouse:  func(msg tea.MouseMsg) tea.Cmd { return b.git.HandleMouse(msg) },
		},
		{
			active: b.zellij.Active,
			view:   func(_, _, _ int) string { return b.zellij.View() },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b, b.zellij.Update(msg) },
		},
		{
			active: func() bool { return b.mnemonic.active },
			view:   func(_, _, _ int) string { return "" },
			key:    func(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return b.mnemonicSelector().handleKey(msg) },
		},
	}
}

func (b *Board) activeModalLayer() *modalLayer {
	layers := b.modalLayers()
	for i := range layers {
		layer := layers[i]
		if layer.active() {
			return &layer
		}
	}
	return nil
}
