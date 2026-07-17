package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
)

type recordingLogger struct{ records []LogRecord }

func (l *recordingLogger) Log(level, source, message string) {
	l.records = append(l.records, LogRecord{Level: level, Source: source, Message: message})
}

func TestDebugPrintCapturesSourceAndInspectsTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FolderInitFile)
	body := `
local value = {z = 2, a = 1}
value.self = value
print("state", value)
captured = kbrd.inspect(value)
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := &recordingLogger{}
	host, err := New(config.ScriptingConfig{Enabled: true}, &fakeAPI{root: dir}, logger, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if len(logger.records) != 1 {
		t.Fatalf("records = %+v", logger.records)
	}
	record := logger.records[0]
	if record.Level != "debug" || !strings.Contains(record.Source, ".kbrd.lua:4") {
		t.Fatalf("record source = %+v", record)
	}
	if !strings.Contains(record.Message, `"a" = 1`) || !strings.Contains(record.Message, `"z" = 2`) || !strings.Contains(record.Message, "<cycle>") {
		t.Fatalf("record message = %q", record.Message)
	}
	if got := luaString(t, host, "captured"); !strings.Contains(got, "<cycle>") {
		t.Fatalf("inspect result = %q", got)
	}
}

func TestInitErrorsSeparateGlobalAndFolderFailures(t *testing.T) {
	globalRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalRoot)
	t.Setenv("HOME", globalRoot)
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	globalDir := filepath.Join(userConfigDir, config.AppDirName)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, GlobalInitFile), []byte(`error("global")`), 0o644); err != nil {
		t.Fatal(err)
	}
	boardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(boardDir, FolderInitFile), []byte(`error("folder")`), 0o644); err != nil {
		t.Fatal(err)
	}
	host, err := New(config.ScriptingConfig{Enabled: true}, &fakeAPI{root: boardDir}, &recordingLogger{}, boardDir, "")
	if host == nil || err == nil {
		t.Fatalf("host, err = %v, %v", host, err)
	}
	defer host.Close()
	globalErr, folderErr := InitErrors(err)
	if globalErr == nil || !strings.Contains(globalErr.Error(), "global") {
		t.Fatalf("global error = %v", globalErr)
	}
	if folderErr == nil || !strings.Contains(folderErr.Error(), "folder") {
		t.Fatalf("folder error = %v", folderErr)
	}
}
