package mcpsetup

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

func validateTOML(path string, data []byte) error {
	return validateTOMLBytes(path, data)
}

func validateTOMLBytes(path string, data []byte) error {
	var out map[string]any
	if err := toml.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("%s: invalid TOML: %w", path, err)
	}
	return nil
}

func quoteTOMLString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func appendTOMLBlock(data []byte, block string) []byte {
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return []byte(block)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if !strings.HasSuffix(text, "\n\n") {
		text += "\n"
	}
	return []byte(text + block)
}

func ensureTOMLTableKeys(data []byte, table string, keys map[string]string) (bool, []byte, error) {
	lines := splitLines(string(data))
	start, end := findTOMLTable(lines, table)
	if start == -1 {
		names := sortedKeys(keys)
		var b strings.Builder
		b.WriteString("[")
		b.WriteString(table)
		b.WriteString("]\n")
		for _, key := range names {
			b.WriteString(key)
			b.WriteString(" = ")
			b.WriteString(keys[key])
			b.WriteByte('\n')
		}
		return true, appendTOMLBlock(data, b.String()), nil
	}

	changed := false
	seen := map[string]bool{}
	for i := start + 1; i < end; i++ {
		key, ok := tomlAssignmentKey(lines[i])
		if !ok {
			continue
		}
		want, has := keys[key]
		if !has {
			continue
		}
		seen[key] = true
		next := leadingWhitespace(lines[i]) + key + " = " + want + lineEnding(lines[i])
		if lines[i] != next {
			lines[i] = next
			changed = true
		}
	}
	var inserts []string
	for _, key := range sortedKeys(keys) {
		if !seen[key] {
			inserts = append(inserts, key+" = "+keys[key]+"\n")
		}
	}
	if len(inserts) > 0 {
		changed = true
		lines = append(lines[:end], append(inserts, lines[end:]...)...)
	}
	if !changed {
		return false, data, nil
	}
	return true, []byte(strings.Join(lines, "")), nil
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func findTOMLTable(lines []string, table string) (int, int) {
	for i, line := range lines {
		name, ok := tomlTableName(line)
		if !ok || name != table {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			nextName, ok := tomlTableName(lines[j])
			if ok && nextName != table && !strings.HasPrefix(nextName, table+".") {
				end = j
				break
			}
		}
		return i, end
	}
	return -1, len(lines)
}

func tomlTableName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[[") || !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	close := strings.Index(trimmed, "]")
	if close < 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[1:close]), true
}

func tomlAssignmentKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	idx := strings.Index(trimmed, "=")
	if idx < 0 {
		return "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" || strings.ContainsAny(key, ".[") {
		return "", false
	}
	return key, true
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func lineEnding(s string) string {
	if strings.HasSuffix(s, "\n") {
		return "\n"
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
