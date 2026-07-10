// Package theme holds the shared color palette used across the UI. It is a
// leaf package in kbrd's internal dependency graph, so feature packages
// (model, git, …) can share presentation primitives without importing each
// other.
package theme

import "charm.land/lipgloss/v2"

// Color keeps palette definitions readable as string literals while satisfying
// Lip Gloss v2's color.Color-based style API.
type Color string

func (c Color) RGBA() (r, g, b, a uint32) {
	return lipgloss.Color(string(c)).RGBA()
}

// Palette holds every color used by the UI, organized by semantic role.
// All lipgloss styling should reference a Palette rather than embedding hex
// literals.
type Palette struct {
	// Foregrounds (light → dim)
	FgStrong   Color // most prominent text (active column name)
	FgEmphasis Color // bold text, titles
	FgBase     Color // default text
	FgSoft     Color // h3, italics, paths
	FgMuted    Color // descriptions, meta
	FgSubtle   Color // unselected preview, code-lang
	FgDim      Color // placeholders, separators
	FgInverse  Color // text on filled primary background (selected row)
	FgOnAccent Color // text on filled accent button (white-ish)

	// Borders
	BorderActive Color // focused/selected panel border
	BorderMuted  Color // inactive border

	// Primary accent + companions
	Primary           Color // main accent (titles, prompts, gutter)
	PrimaryStrong     Color // filled selection bg, active border
	AccentSoft        Color // h2, meta when selected
	FgSelectedPreview Color // preview text when row is selected+active
	BgSelectedDetail  Color // detail bg when row is selected+active
	Link              Color // markdown link
	AccentAlt         Color // secondary accent (moved item, special border)

	// Semantic state
	Success     Color
	Danger      Color
	Warning     Color
	WarningSoft Color // pins, inline code fg
	Highlight   Color // mnemonic, cursor, fuzzy match

	// Code blocks (markdown)
	BgCodeInline Color
	BgCodeBlock  Color
	FgCodeBlock  Color
}
