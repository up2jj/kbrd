package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type customWatchTarget struct {
	Dir          string
	Path         string
	ReloadConfig bool
	AddDir       string
}

func (b *Board) watchPaths() ([]string, error) {
	paths, err := boardWatchPaths(b.cfg.Path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(paths)+2)
	add := func(path string) {
		if path == "" || seen[path] || !watchDirExists(path) {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	for _, path := range paths {
		add(path)
	}
	for _, target := range b.customWatchTargets() {
		if target.AddDir != "" && watchDirExists(target.AddDir) {
			continue
		}
		add(target.Dir)
	}
	return out, nil
}

func boardWatchPaths(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	paths := []string{root}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		paths = append(paths, filepath.Join(root, name))
	}
	sort.Strings(paths[1:])
	return paths, nil
}

func (b *Board) customWatchTargets() []customWatchTarget {
	global, err := globalConfigPath()
	if err != nil {
		return nil
	}
	appDir := filepath.Dir(global)
	parent := filepath.Dir(appDir)
	return []customWatchTarget{
		{Dir: appDir, Path: global, ReloadConfig: true},
		{Dir: parent, Path: appDir, ReloadConfig: true, AddDir: appDir},
	}
}

func (b *Board) watchEventForPath(path string) (watchEventMsg, bool) {
	for _, target := range b.customWatchTargets() {
		if !samePath(path, target.Path) {
			continue
		}
		if target.AddDir != "" && b.watcher != nil && watchDirExists(target.AddDir) {
			_ = b.watcher.Add(target.AddDir)
		}
		msgPath := target.Path
		if target.ReloadConfig {
			if global, err := globalConfigPath(); err == nil {
				msgPath = global
			}
		}
		return watchEventMsg{Path: msgPath, ReloadConfig: target.ReloadConfig}, true
	}
	if !b.isBoardWatchPath(path) {
		return watchEventMsg{}, false
	}
	return watchEventMsg{Path: path}, true
}

func (b *Board) isBoardWatchPath(path string) bool {
	if path == "" {
		return true
	}
	if samePath(filepath.Dir(path), b.cfg.Path) {
		return true
	}
	for _, col := range b.columns {
		if samePath(filepath.Dir(path), col.Path) {
			return true
		}
	}
	return false
}

func watchDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
