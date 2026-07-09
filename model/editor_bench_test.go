package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func openBenchVimEditor(b *testing.B, content string) (*Editor, string) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatalf("write: %v", err)
	}
	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)
	return e, path
}

func BenchmarkVimEditorIsDirtyLargeFile(b *testing.B) {
	e, _ := openBenchVimEditor(b, strings.Repeat("body line\n", 5000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.IsDirty()
	}
}

func BenchmarkVimSwapMovementAfterEdit(b *testing.B) {
	e, _ := openBenchVimEditor(b, strings.Repeat("body line\n", 1000))
	e.Update(runeKey('x')) // dirty once and write the swap

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Update(runeKey('l'))
	}
}

func BenchmarkVimSwapFlushAfterEdit(b *testing.B) {
	e, _ := openBenchVimEditor(b, strings.Repeat("body line\n", 1000))
	e.Update(runeKey('A'))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	}
}
