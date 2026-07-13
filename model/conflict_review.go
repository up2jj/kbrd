package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	kbrdfs "kbrd/fs"
)

type conflictReviewAction int

const (
	conflictKeepOriginal conflictReviewAction = iota
	conflictReplaceOriginal
	conflictKeepBoth
)

type conflictReviewActionMsg struct {
	Action   conflictReviewAction
	Conflict kbrdfs.Conflict
	Name     string
}

type conflictReviewEditMsg struct {
	Conflict kbrdfs.Conflict
}

type conflictReviewDiffMsg struct {
	IncomingPath string
	Text         string
	Err          error
}

// ConflictReview is the modal inbox for sync-generated incoming card copies.
// Filesystem mutations are returned as messages so the Board remains the
// owner of notifications and reloads.
type ConflictReview struct {
	active      bool
	repoRoot    string
	conflicts   []kbrdfs.Conflict
	nav         groupedMenuNav
	palette     Palette
	width       int
	height      int
	diff        Peek
	showingDiff bool
	name        string
	form        *huh.Form
	escArmed    bool
}

func (r *ConflictReview) Active() bool { return r.active }

func (r *ConflictReview) SetPalette(p Palette) {
	r.palette = p
	r.diff.palette = p
}

func (r *ConflictReview) SetSize(w, h int) {
	r.width = w
	r.height = h
}

func (r *ConflictReview) Open(repoRoot string) error {
	conflicts, err := kbrdfs.ListConflicts(repoRoot)
	if err != nil {
		return err
	}
	if len(conflicts) == 0 {
		return fmt.Errorf("no pending conflicts")
	}
	r.active = true
	r.repoRoot = repoRoot
	r.conflicts = conflicts
	r.showingDiff = false
	r.form = nil
	r.escArmed = false
	r.diff.Close()
	r.resetNavigation(0)
	return nil
}

func (r *ConflictReview) Close() {
	r.active = false
	r.repoRoot = ""
	r.conflicts = nil
	r.nav.Reset()
	r.showingDiff = false
	r.form = nil
	r.escArmed = false
	r.diff.Close()
}

// Refresh reloads the pending inventory after a resolution, preserving the
// selected incoming path when it still exists.
func (r *ConflictReview) Refresh() error {
	if r.repoRoot == "" {
		return nil
	}
	selected := ""
	selectedIndex := min(max(r.nav.selected, 0), max(len(r.conflicts)-1, 0))
	if conflict, ok := r.selectedConflict(); ok {
		selected = conflict.IncomingPath
	}
	conflicts, err := kbrdfs.ListConflicts(r.repoRoot)
	if err != nil {
		return err
	}
	if len(conflicts) == 0 {
		r.Close()
		return nil
	}
	r.conflicts = conflicts
	selectedIndex = min(selectedIndex, max(len(conflicts)-1, 0))
	for i, conflict := range conflicts {
		if conflict.IncomingPath == selected {
			selectedIndex = i
			break
		}
	}
	r.resetNavigation(selectedIndex)
	return nil
}

func (r *ConflictReview) resetNavigation(selected int) {
	r.nav.Reset()
	for i := range r.conflicts {
		r.nav.nav = append(r.nav.nav, i)
	}
	r.nav.selected = min(max(selected, 0), max(len(r.nav.nav)-1, 0))
}

func (r *ConflictReview) selectedConflict() (kbrdfs.Conflict, bool) {
	row, ok := r.nav.SelectedRow()
	if !ok || row < 0 || row >= len(r.conflicts) {
		return kbrdfs.Conflict{}, false
	}
	return r.conflicts[row], true
}

func (r *ConflictReview) Update(msg tea.KeyPressMsg) tea.Cmd {
	if r.showingDiff {
		switch {
		case key.Matches(msg, Keys.PeekClose):
			r.showingDiff = false
			r.diff.Close()
		case msg.String() == "k":
			return r.action(conflictKeepOriginal, "")
		case msg.String() == "r":
			return r.action(conflictReplaceOriginal, "")
		case msg.String() == "b":
			return r.startKeepBoth()
		case msg.String() == "e":
			if conflict, ok := r.selectedConflict(); ok {
				r.showingDiff = false
				r.diff.Close()
				return func() tea.Msg { return conflictReviewEditMsg{Conflict: conflict} }
			}
		default:
			r.diff.Update(msg)
		}
		return nil
	}
	if r.form != nil {
		return r.updateNameForm(msg)
	}

	switch {
	case key.Matches(msg, Keys.PeekClose):
		r.Close()
	case msg.String() == "enter", msg.String() == "d":
		return r.openDiff()
	case msg.String() == "e":
		if conflict, ok := r.selectedConflict(); ok {
			r.Close()
			return func() tea.Msg { return conflictReviewEditMsg{Conflict: conflict} }
		}
	case msg.String() == "k":
		return r.action(conflictKeepOriginal, "")
	case msg.String() == "r":
		return r.action(conflictReplaceOriginal, "")
	case msg.String() == "b":
		return r.startKeepBoth()
	case msg.String() == "s":
		r.nav.UpdateKey("down")
	case key.Matches(msg, Keys.CursorUp):
		r.nav.UpdateKey("up")
	case key.Matches(msg, Keys.CursorDown):
		r.nav.UpdateKey("down")
	case key.Matches(msg, Keys.PeekTop):
		r.nav.UpdateKey("home")
	case key.Matches(msg, Keys.PeekBottom):
		r.nav.UpdateKey("end")
	}
	return nil
}

func (r *ConflictReview) action(action conflictReviewAction, name string) tea.Cmd {
	conflict, ok := r.selectedConflict()
	if !ok {
		return nil
	}
	if r.showingDiff {
		r.showingDiff = false
		r.diff.Close()
	}
	return func() tea.Msg {
		return conflictReviewActionMsg{Action: action, Conflict: conflict, Name: name}
	}
}

func (r *ConflictReview) startKeepBoth() tea.Cmd {
	conflict, ok := r.selectedConflict()
	if !ok {
		return nil
	}
	if r.showingDiff {
		r.showingDiff = false
		r.diff.Close()
	}
	base := strings.TrimSuffix(filepath.Base(conflict.OriginalPath), filepath.Ext(conflict.OriginalPath))
	r.name = base + " (incoming)"
	r.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Key("name").
			Title("New card name").
			Description("keep the incoming version as a separate card").
			Value(&r.name).
			Validate(func(value string) error {
				value = strings.TrimSpace(strings.TrimSuffix(value, ".md"))
				if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) || strings.Contains(value, "..") {
					return fmt.Errorf("enter a safe card name")
				}
				return nil
			}),
	)).WithTheme(huhThemeFor(r.palette)).WithShowHelp(false)
	r.escArmed = false
	return r.form.Init()
}

func (r *ConflictReview) updateNameForm(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "esc" {
		if r.escArmed {
			r.form = nil
			r.escArmed = false
			return nil
		}
		r.escArmed = true
	} else {
		r.escArmed = false
	}
	model, cmd := r.form.Update(msg)
	if form, ok := model.(*huh.Form); ok {
		r.form = form
	}
	switch r.form.State {
	case huh.StateCompleted:
		conflict, ok := r.selectedConflict()
		name := strings.TrimSpace(strings.TrimSuffix(r.name, ".md"))
		r.form = nil
		r.escArmed = false
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return conflictReviewActionMsg{Action: conflictKeepBoth, Conflict: conflict, Name: name}
		}
	case huh.StateAborted:
		r.form = nil
		r.escArmed = false
		return nil
	}
	return cmd
}

func (r *ConflictReview) openDiff() tea.Cmd {
	conflict, ok := r.selectedConflict()
	if !ok {
		return nil
	}
	root := r.repoRoot
	return func() tea.Msg {
		left := filepath.Join(root, filepath.FromSlash(conflict.OriginalPath))
		if _, err := os.Stat(left); os.IsNotExist(err) {
			left = "/dev/null"
		}
		right := filepath.Join(root, filepath.FromSlash(conflict.IncomingPath))
		text, err := kbrdfs.GitDiffFiles(root, left, right)
		return conflictReviewDiffMsg{IncomingPath: conflict.IncomingPath, Text: text, Err: err}
	}
}

func (r *ConflictReview) showDiff(msg conflictReviewDiffMsg) {
	if !r.active || msg.Err != nil {
		return
	}
	r.diff.palette = r.palette
	r.diff.Open("Diff · "+filepath.Base(msg.IncomingPath), msg.Text, r.width)
	r.showingDiff = true
}

func (r *ConflictReview) View(termWidth, termHeight int) string {
	if !r.active {
		return ""
	}
	if r.showingDiff {
		return r.diff.ViewWithHints(termWidth, termHeight, []Shortcut{
			{"↑/↓", "scroll"}, {"k", "keep original"}, {"r", "use incoming"},
			{"b", "keep both"}, {"e", "edit"}, {"q/esc", "back"},
		})
	}
	if r.form != nil {
		return OverlayFrame{
			Title: "Keep both", Body: r.form.View(),
			Footer:  RenderInlineHints([]Shortcut{{"enter", "keep both"}, {"esc", "cancel"}}),
			Palette: r.palette,
		}.Render()
	}
	p := r.palette
	footer := RenderInlineHints([]Shortcut{
		{"↑/↓", "select"}, {"enter", "compare"}, {"q/esc", "close"},
	})
	textW := max(lipgloss.Width(footer)+4, 58)
	for _, conflict := range r.conflicts {
		textW = max(textW, lipgloss.Width(r.conflictLabel(conflict))+4)
	}
	if termWidth > 0 {
		textW = min(textW, max(termWidth-8, 1))
	}
	body, pos := renderGroupedPickerBody(groupedPickerBody{
		Palette: p, Rows: len(r.conflicts), TermHeight: termHeight, TextWidth: textW, Compact: true,
		Nav: &r.nav, RenderRow: func(row int, selected bool) string {
			return r.renderRow(r.conflicts[row], selected, textW)
		},
	})
	gap := max(textW-lipgloss.Width(footer)-lipgloss.Width(pos), 1)
	return OverlayFrame{
		Title: fmt.Sprintf("Review Changes · %d pending", len(r.conflicts)),
		Body:  body, Footer: footer + strings.Repeat(" ", gap) + pos,
		Width: overlayWidthForBody(textW), Palette: p,
	}.Render()
}

func (r *ConflictReview) renderRow(conflict kbrdfs.Conflict, selected bool, width int) string {
	label := r.conflictLabel(conflict)
	if selected {
		label = lipgloss.NewStyle().Bold(true).Foreground(r.palette.Highlight).Render("> " + label)
	} else {
		label = lipgloss.NewStyle().Foreground(r.palette.FgSoft).Render("  " + label)
	}
	return lipgloss.NewStyle().Width(max(width, 1)).Render(label)
}

func (r *ConflictReview) conflictLabel(conflict kbrdfs.Conflict) string {
	source := conflict.Label
	if source == "" {
		source = "another device"
	}
	if conflict.Sequence > 1 {
		source += fmt.Sprintf(" #%d", conflict.Sequence)
	}
	return filepath.Base(conflict.OriginalPath) + "  ←  incoming from " + source
}
