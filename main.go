package main

import (
	"flag"
	"fmt"
	"os"

	"kbrd/config"
	"kbrd/model"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	path := flag.String("path", ".", "path to folder to monitor (defaults to current directory)")
	previewLines := flag.Int("preview-lines", 3, "number of preview lines to show")
	flag.Parse()

	info, err := os.Stat(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot access path: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "error: path is not a directory\n")
		os.Exit(1)
	}

	cfg := config.Config{
		Path:         *path,
		PreviewLines: *previewLines,
		Theme:        "light",
	}

	m := model.NewBoard(cfg)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
