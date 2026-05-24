package model

import "github.com/charmbracelet/lipgloss"

// Palette holds every color used by the UI, organized by semantic role.
// All lipgloss styling in the model package should reference a Palette
// rather than embedding hex literals.
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
	PrimaryStrong    lipgloss.Color // filled selection bg, active border
	AccentSoft       lipgloss.Color // h2, meta when selected
	FgSelectedPreview lipgloss.Color // preview text when row is selected+active
	BgSelectedDetail lipgloss.Color // detail bg when row is selected+active
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

// DarkPalette returns the dark theme — the exact color set the codebase used
// before the theme refactor, preserved verbatim so existing screenshots match.
func DarkPalette() Palette {
	return Palette{
		FgStrong:   "#f8fafc",
		FgEmphasis: "#f1f5f9",
		FgBase:     "#e2e8f0",
		FgSoft:     "#cbd5e1",
		FgMuted:    "#94a3b8",
		FgSubtle:   "#64748b",
		FgDim:      "#475569",
		FgInverse:  "#0f172a",
		FgOnAccent: "#ffffff",

		BorderActive: "#3b82f6",
		BorderMuted:  "#334155",

		Primary:           "#60a5fa",
		PrimaryStrong:    "#3b82f6",
		AccentSoft:       "#93c5fd",
		FgSelectedPreview: "#bfdbfe",
		BgSelectedDetail: "#1e3a8a",
		Link:              "#38bdf8",
		AccentAlt:         "#a78bfa",

		Success:     "#22c55e",
		Danger:      "#ef4444",
		Warning:     "#f59e0b",
		WarningSoft: "#fbbf24",
		Highlight:   "#fde047",

		BgCodeInline: "#1f2937",
		BgCodeBlock:  "#111827",
		FgCodeBlock:  "#e5e7eb",
	}
}

// LightPalette returns a light theme — dark text on light surfaces.
// Accent hues mirror the dark theme so brand feel is consistent.
func LightPalette() Palette {
	return Palette{
		FgStrong:   "#020617", // slate-950
		FgEmphasis: "#0f172a", // slate-900
		FgBase:     "#1e293b", // slate-800
		FgSoft:     "#334155", // slate-700
		FgMuted:    "#475569", // slate-600
		FgSubtle:   "#64748b", // slate-500
		FgDim:      "#94a3b8", // slate-400
		FgInverse:  "#f8fafc", // slate-50 (text on filled primary)
		FgOnAccent: "#ffffff",

		BorderActive: "#2563eb", // blue-600
		BorderMuted:  "#cbd5e1", // slate-300

		Primary:           "#2563eb", // blue-600
		PrimaryStrong:    "#1d4ed8", // blue-700
		AccentSoft:       "#3b82f6", // blue-500
		FgSelectedPreview: "#1e3a8a", // blue-900
		BgSelectedDetail: "#dbeafe", // blue-100
		Link:              "#0284c7", // sky-600
		AccentAlt:         "#7c3aed", // violet-600

		Success:     "#15803d", // green-700
		Danger:      "#b91c1c", // red-700
		Warning:     "#b45309", // amber-700
		WarningSoft: "#a16207", // yellow-700
		Highlight:   "#ca8a04", // yellow-600

		BgCodeInline: "#f1f5f9", // slate-100
		BgCodeBlock:  "#e2e8f0", // slate-200
		FgCodeBlock:  "#0f172a", // slate-900
	}
}

// PaletteFor returns the palette matching the named theme. Unknown names
// fall back to the dark palette.
func PaletteFor(name string) Palette {
	switch name {
	case "light":
		return LightPalette()
	default:
		return DarkPalette()
	}
}

// applyPackageStyles recomputes package-level style variables that are shared
// across many call sites (help row styles, markdown styles). Board.applyPalette
// calls this whenever the active palette changes.
func applyPackageStyles(p Palette) {
	setHelpStyles(p)
	setMarkdownStyles(p)
}

func init() {
	applyPackageStyles(DarkPalette())
}
