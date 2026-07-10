package theme

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// ApplyTextInputPalette restyles a bubbles text input using the app palette.
// Keeping this with Palette ensures inputs in separate UI packages remain
// visually consistent when the terminal theme changes.
func ApplyTextInputPalette(input *textinput.Model, palette Palette) {
	styles := input.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(palette.Primary).Bold(true)
	styles.Blurred.Prompt = styles.Focused.Prompt
	styles.Focused.Text = lipgloss.NewStyle().Foreground(palette.FgBase)
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(palette.FgDim).Italic(true)
	styles.Blurred.Placeholder = styles.Focused.Placeholder
	styles.Cursor.Color = palette.Highlight
	input.SetStyles(styles)
}
