package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	cardhistory "kbrd/history"
)

func TestTimelineRenderRowIncludesHourAndMinute(t *testing.T) {
	timeline := Timeline{palette: DarkPalette()}
	event := cardhistory.Event{
		Time:    time.Date(2026, 7, 13, 14, 37, 0, 0, time.UTC),
		Type:    cardhistory.EventEdited,
		Summary: "Edited",
	}

	row := ansi.Strip(timeline.renderRow(event, false, 66))
	if !strings.Contains(row, "14:37") {
		t.Fatalf("row = %q, want hour and minute", row)
	}
}

func TestWriteRestoredCopyNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	preferred := filepath.Join(dir, "task (restored Jul 13).md")
	if err := os.WriteFile(preferred, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := writeRestoredCopy(preferred, []byte("snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "task (restored Jul 13) 2.md" {
		t.Fatalf("path = %q", path)
	}
	data, _ := os.ReadFile(preferred)
	if string(data) != "original" {
		t.Fatalf("existing copy overwritten: %q", data)
	}
}

func TestRestoredCopyPath(t *testing.T) {
	e := cardhistory.Event{Time: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)}
	got := restoredCopyPath(filepath.Join("Doing", "task.md"), e)
	if got != filepath.Join("Doing", "task (restored Jul 13).md") {
		t.Fatalf("path = %q", got)
	}
}
