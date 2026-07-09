package vimbuf

import (
	"strings"
	"testing"

	"kbrd/theme"
)

func largeWrappedText(lines int) string {
	line := strings.Repeat("verylongword ", 24)
	parts := make([]string, lines)
	for i := range parts {
		parts[i] = line
	}
	return strings.Join(parts, "\n")
}

func BenchmarkVimBufferInsertLargeFile(b *testing.B) {
	buf := New(largeWrappedText(2000))
	buf.SetSize(100, 36)
	buf.GoToLine(1000)
	buf.HandleKey("A")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.HandleKey("x")
	}
}

func BenchmarkVimBufferRenderLargeWrappedFile(b *testing.B) {
	buf := New(largeWrappedText(3000))
	buf.SetSize(100, 36)
	buf.GoToLine(2500)
	p := theme.Palette{
		FgDim:            theme.Color("#667085"),
		Primary:          theme.Color("#7c9cff"),
		BgSelectedDetail: theme.Color("#263248"),
		Highlight:        theme.Color("#ffd166"),
		FgInverse:        theme.Color("#101828"),
		FgBase:           theme.Color("#f2f4f7"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.View(p)
	}
}
