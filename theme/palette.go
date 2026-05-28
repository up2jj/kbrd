// Package theme holds the shared color palette used across the UI. It is a
// dependency-free leaf package so feature packages (model, git, …) can all
// reference one Palette type without importing each other.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette holds every color used by the UI, organized by semantic role.
// All lipgloss styling should reference a Palette rather than embedding hex
// literals.
type Palette struct {
	// Foregrounds (light → dim)
	FgStrong   lipgloss.Color // most prominent text (active column name)
	FgEmphasis lipgloss.Color // bold text, titles
	FgBase     lipgloss.Color // default text
	FgSoft     lipgloss.Color // h3, italics, paths
	FgMuted    lipgloss.Color // descriptions, meta
	FgSubtle   lipgloss.Color // unselected preview, code-lang
	FgDim      lipgloss.Color // placeholders, separators
	FgInverse  lipgloss.Color // text on filled primary background (selected row)
	FgOnAccent lipgloss.Color // text on filled accent button (white-ish)

	// Borders
	BorderActive lipgloss.Color // focused/selected panel border
	BorderMuted  lipgloss.Color // inactive border

	// Primary accent + companions
	Primary           lipgloss.Color // main accent (titles, prompts, gutter)
	PrimaryStrong     lipgloss.Color // filled selection bg, active border
	AccentSoft        lipgloss.Color // h2, meta when selected
	FgSelectedPreview lipgloss.Color // preview text when row is selected+active
	BgSelectedDetail  lipgloss.Color // detail bg when row is selected+active
	Link              lipgloss.Color // markdown link
	AccentAlt         lipgloss.Color // secondary accent (moved item, special border)

	// Semantic state
	Success     lipgloss.Color
	Danger      lipgloss.Color
	Warning     lipgloss.Color
	WarningSoft lipgloss.Color // pins, inline code fg
	Highlight   lipgloss.Color // mnemonic, cursor, fuzzy match

	// Code blocks (markdown)
	BgCodeInline lipgloss.Color
	BgCodeBlock  lipgloss.Color
	FgCodeBlock  lipgloss.Color
}
