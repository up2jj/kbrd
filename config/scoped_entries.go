package config

import (
	"fmt"
	"path/filepath"
)

// loadScopedEntries reads global and board-local entries, lets every local ID
// override its global counterpart, and reports duplicate IDs within either
// resulting layer. Decoding and validation stay with each entry type.
func loadScopedEntries[T any](globalDir, folderPath, globalFile, folderFile string, read func(string) ([]T, []CommandLoadWarning, error), id, name func(T) string) ([]T, []CommandLoadWarning, error) {
	var warnings []CommandLoadWarning

	var global []T
	if globalDir != "" {
		entries, ws, err := read(filepath.Join(globalDir, globalFile))
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		global = entries
	}

	var local []T
	if folderPath != "" {
		entries, ws, err := read(filepath.Join(folderPath, folderFile))
		if err != nil {
			return nil, warnings, err
		}
		warnings = append(warnings, ws...)
		local = entries
	}

	merged := mergeScopedEntries(global, local, id)
	warnings = append(warnings, duplicateIDWarnings(merged, folderFile, id, name)...)
	return merged, warnings, nil
}

func mergeScopedEntries[T any](global, local []T, id func(T) string) []T {
	merged := make([]T, 0, len(global)+len(local))
	overridden := make(map[string]bool, len(local))
	for _, entry := range local {
		overridden[id(entry)] = true
	}
	for _, entry := range global {
		if overridden[id(entry)] {
			continue
		}
		merged = append(merged, entry)
	}
	return append(merged, local...)
}

func duplicateIDWarnings[T any](entries []T, source string, id, name func(T) string) []CommandLoadWarning {
	var warnings []CommandLoadWarning
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		entryID, entryName := id(entry), name(entry)
		if winner, ok := seen[entryID]; ok {
			warnings = append(warnings, CommandLoadWarning{
				Source:  source,
				Message: fmt.Sprintf("duplicate id %q: %q shadowed by %q", entryID, entryName, winner),
			})
			continue
		}
		seen[entryID] = entryName
	}
	return warnings
}
