package model

import (
	"os/exec"
	"sort"
	"strings"
	"testing"
)

func TestBoardSurfaceAudit(t *testing.T) {
	out, err := exec.Command("rg", "-n", "^func \\(b \\*Board\\)", ".").Output()
	if err != nil {
		t.Skipf("rg unavailable or no Board methods found: %v", err)
	}
	byFile := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		file, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		byFile[file]++
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	var b strings.Builder
	for _, file := range files {
		b.WriteString(file)
		b.WriteString(": ")
		b.WriteString(strings.Repeat("#", byFile[file]))
		b.WriteString("\n")
	}
	t.Logf("remaining Board method surface by file:\n%s", b.String())
}
