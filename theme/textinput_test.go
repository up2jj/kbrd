package theme

import (
	"testing"

	"charm.land/bubbles/v2/textinput"
)

func TestApplyTextInputPalette(t *testing.T) {
	palette := Palette{
		Primary:   "primary",
		FgBase:    "base",
		FgDim:     "dim",
		Highlight: "highlight",
	}
	input := textinput.New()

	ApplyTextInputPalette(&input, palette)
	styles := input.Styles()
	if got := styles.Focused.Prompt.GetForeground(); got != palette.Primary || !styles.Focused.Prompt.GetBold() {
		t.Fatalf("focused prompt style = foreground %v, bold %t", got, styles.Focused.Prompt.GetBold())
	}
	if got := styles.Focused.Text.GetForeground(); got != palette.FgBase {
		t.Fatalf("focused text foreground = %v, want %v", got, palette.FgBase)
	}
	if got := styles.Focused.Placeholder.GetForeground(); got != palette.FgDim || !styles.Focused.Placeholder.GetItalic() {
		t.Fatalf("focused placeholder style = foreground %v, italic %t", got, styles.Focused.Placeholder.GetItalic())
	}
	if got := styles.Cursor.Color; got != palette.Highlight {
		t.Fatalf("cursor color = %v, want %v", got, palette.Highlight)
	}
}
