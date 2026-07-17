package script

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFileLoggerRotatesAndRetainsThreeArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxScriptLogSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"one", "two", "three"} {
		if err := os.WriteFile(path+"."+strconv.Itoa(i+1), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := &FileLogger{path: path}
	l.Log("debug", "test", "rotated")
	l.Close()

	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth archive: %v", err)
	}
	if got, _ := os.ReadFile(path + ".1"); len(got) != maxScriptLogSize {
		t.Fatalf("archive .1 size = %d", len(got))
	}
	if got, _ := os.ReadFile(path + ".2"); string(got) != "one" {
		t.Fatalf("archive .2 = %q", got)
	}
	if got, _ := os.ReadFile(path + ".3"); string(got) != "two" {
		t.Fatalf("archive .3 = %q", got)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "rotated") {
		t.Fatalf("active log = %q", got)
	}
}

func TestFileLoggerContinuesWhenArchiveShiftFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxScriptLogSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".2", 0o755); err != nil {
		t.Fatal(err)
	}
	l := &FileLogger{path: path}
	l.Log("debug", "test", "fallback")
	l.Close()
	got, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), "fallback") {
		t.Fatalf("fallback log = %q, %v", got, err)
	}
}
