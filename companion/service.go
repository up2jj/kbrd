// Package companion exposes the small, headless surface used by the macOS
// menu-bar companion. It deliberately reuses the same stores as the TUI.
package companion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kbrd/board"
	"kbrd/clipboardring"
	"kbrd/config"
	kbrdfs "kbrd/fs"
	"kbrd/recents"
	"kbrd/scratchpad"
)

const maxClipboardEntries = 50

type Snapshot struct {
	Boards    []BoardSnapshot       `json:"boards"`
	Clipboard []clipboardring.Entry `json:"clipboard"`
}

type BoardSnapshot struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Columns    []string `json:"columns"`
	Available  bool     `json:"available"`
	Scratchpad string   `json:"scratchpad,omitempty"`
	Git        string   `json:"git"`
	Reminders  string   `json:"reminders"`
}

func LoadSnapshot() (Snapshot, error) {
	store, err := recents.Load()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load recent boards: %w", err)
	}
	pad, err := scratchpad.Open("")
	if err != nil {
		return Snapshot{}, err
	}

	result := Snapshot{Boards: make([]BoardSnapshot, 0, len(store.Entries))}
	for _, entry := range store.Entries {
		item := BoardSnapshot{Name: entry.Name, Path: entry.Path}
		if item.Name == "" {
			item.Name = filepath.Base(entry.Path)
		}
		columns, columnsErr := board.Columns(entry.Path)
		if columnsErr != nil {
			item.Git = "unavailable"
			item.Reminders = "unavailable"
			result.Boards = append(result.Boards, item)
			continue
		}
		item.Available = true
		item.Columns = columns
		item.Scratchpad, _ = pad.Load(entry.Path)
		item.Git = gitStatus(entry.Path)
		cfg, cfgErr := config.Load(entry.Path)
		if cfgErr != nil {
			item.Reminders = "configuration error"
		} else if cfg.Reminders.Enabled {
			item.Reminders = remindersStatus(entry.Path, cfg.Reminders.List)
		} else {
			item.Reminders = "off"
		}
		result.Boards = append(result.Boards, item)
	}

	ring, err := clipboardring.Open("")
	if err != nil {
		return Snapshot{}, fmt.Errorf("load clipboard history: %w", err)
	}
	entries := ring.Entries()
	if len(entries) > maxClipboardEntries {
		entries = entries[:maxClipboardEntries]
	}
	result.Clipboard = entries
	return result, nil
}

func AppendScratchpad(boardSelector, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("scratchpad text is empty")
	}
	ref, err := board.ResolveExisting(boardSelector)
	if err != nil {
		return err
	}
	store, err := scratchpad.Open("")
	if err != nil {
		return err
	}
	if _, err := store.Append(ref.Path, text); err != nil {
		return fmt.Errorf("append scratchpad: %w", err)
	}
	return nil
}

func gitStatus(boardPath string) string {
	repo := kbrdfs.GitRepoRoot(boardPath)
	if repo == "" {
		return "not a repository"
	}
	changes := len(kbrdfs.GitChangedFiles(repo))
	remote := kbrdfs.GitHasRemote(repo)
	if changes > 0 {
		return fmt.Sprintf("%d changed", changes)
	}
	if !remote {
		return "clean · local"
	}
	out, err := kbrdfs.GitOutput(repo, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return "clean · remote"
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return "clean · remote"
	}
	ahead, aheadErr := strconv.Atoi(fields[0])
	behind, behindErr := strconv.Atoi(fields[1])
	if aheadErr != nil || behindErr != nil {
		return "clean · remote"
	}
	switch {
	case ahead > 0 && behind > 0:
		return fmt.Sprintf("diverged · %d ahead · %d behind", ahead, behind)
	case ahead > 0:
		return fmt.Sprintf("%d ahead", ahead)
	case behind > 0:
		return fmt.Sprintf("%d behind", behind)
	default:
		return "up to date"
	}
}

func remindersStatus(boardPath, list string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		return "enabled · " + list
	}
	abs, err := filepath.Abs(boardPath)
	if err != nil {
		return "enabled · " + list
	}
	sum := sha256.Sum256([]byte(abs))
	path := filepath.Join(base, config.AppDirName, "reminders", hex.EncodeToString(sum[:12])+".json")
	info, err := os.Stat(path)
	if err != nil {
		return "enabled · not synced"
	}
	age := time.Since(info.ModTime())
	var when string
	switch {
	case age < time.Minute:
		when = "just now"
	case age < time.Hour:
		when = fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		when = fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		when = fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
	return "synced " + when + " · " + list
}
