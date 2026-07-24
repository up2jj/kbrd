package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kbrd/config"
	"kbrd/frontmatter"
	"kbrd/recents"
)

func runIngestCommand(t *testing.T, stdin string, args ...string) (string, error) {
	return runIngestCommandWithFlags(t, cliFlags{}, stdin, args...)
}

func runIngestCommandWithFlags(t *testing.T, flags cliFlags, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newIngestCmd(&flags)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.Execute()
	return out.String(), err
}

func runSafeIngestCommand(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	cmd.SetArgs(append([]string{"--safe", "ingest"}, args...))
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.Execute()
	return out.String(), err
}

func makeIngestBoard(t *testing.T, columns ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range columns {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIngestUsesRecentBoardNumericColumnAndSanitizedName(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "1. TODO", "2. DOING", "_archive")
	store := recents.Store{Entries: []recents.Entry{{Name: "Work", Path: root}}}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := runIngestCommand(t, "# Fix it", "--board", "work", "--column", "2", "--name", "Fix / OAuth: P1!")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "2. DOING", "fix-oauth-p1.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "---\ncreated_at: ") || !strings.HasSuffix(string(data), "---\n\n# Fix it\n") {
		t.Fatalf("content = %q, want created_at frontmatter followed by card body", data)
	}
	if !strings.Contains(out, "ingested fix-oauth-p1.md in [Work] 2. DOING") {
		t.Fatalf("output = %q", out)
	}
}

func TestIngestUsesFilesystemBoardAndFirstColumn(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "2. Doing", "1. Todo")

	_, err := runIngestCommand(t, "", "--board", root, "--name", "A Card.md", "--content", "body")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "1. Todo", "a-card.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "---\n\nbody\n") {
		t.Fatalf("content = %q, want created_at frontmatter followed by card body", data)
	}
}

func TestIngestRecordsSourceInFrontmatter(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "todo")

	_, err := runIngestCommand(t, "", "--board", root, "--name", "Captured", "--content", "body", "--source", "companion")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo", "captured.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nsource: \"companion\"\n") {
		t.Fatalf("content = %q, want companion source frontmatter", data)
	}

	_, err = runIngestCommand(t, "", "--board", root, "--name", "Typed", "--content", "body", "--source", "true")
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(root, "todo", "typed.md"))
	if err != nil {
		t.Fatal(err)
	}
	block, _, fenced := frontmatter.Split(string(data))
	if !fenced {
		t.Fatalf("content = %q, want frontmatter", data)
	}
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := parsed.Data["source"].(string); !ok || got != "true" {
		t.Fatalf("source = %#v, want string %q", parsed.Data["source"], "true")
	}

	_, err = runIngestCommand(t, "", "--board", root, "--name", "Invalid", "--content", "body", "--source", "bad\nsource")
	if err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("invalid source error = %v", err)
	}
}

func TestIngestReadsFileAndRejectsInvalidInputs(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "todo")
	input := filepath.Join(t.TempDir(), "input.md")
	if err := os.WriteFile(input, []byte("from file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runIngestCommand(t, "", "--board", root, "--name", "File", "--file", input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo", "file.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "---\n\nfrom file\n") {
		t.Fatalf("content = %q, want created_at frontmatter followed by card body", data)
	}

	_, err = runIngestCommand(t, "", "--board", root, "--name", "Other", "--content", "x", "--file", input)
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("mutually exclusive error = %v", err)
	}

	_, err = runIngestCommand(t, "", "--board", root, "--column", "2", "--name", "Other", "--content", "x")
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("column range error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "2")); !os.IsNotExist(err) {
		t.Fatalf("missing column was created or stat failed: %v", err)
	}
}

func TestIngestUsesBoardCreatedAtFormat(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "todo")
	if err := os.WriteFile(filepath.Join(root, "kbrd.toml"), []byte("[ingest]\ncreated_at_format = \"2006-01-02\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runIngestCommand(t, "", "--board", root, "--name", "Dated", "--content", "body")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo", "dated.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "created_at: \"" + time.Now().UTC().Format(time.DateOnly) + "\""; !strings.Contains(string(data), want) {
		t.Fatalf("content = %q, want %q", data, want)
	}
}

func TestIngestRunsItemCreatedHooksAndSafeSkipsThem(t *testing.T) {
	isolateConfig(t)
	root := makeIngestBoard(t, "todo")
	hookOutput := filepath.Join(root, "hook-output")
	hooks := "hooks:\n" +
		"  - name: Record created card\n" +
		"    id: record-created\n" +
		"    event: item_created\n" +
		"    command: printf '%s|%s|%s' '{{.filePath}}' '{{.columnName}}' '{{.fileName}}' > '" + hookOutput + "'\n" +
		"  - name: Fails without aborting\n" +
		"    id: failing-created\n" +
		"    event: item_created\n" +
		"    command: false\n"
	if err := os.WriteFile(filepath.Join(root, config.FolderHooksFile), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runIngestCommand(t, "", "--board", root, "--name", "Hook card", "--content", "body"); err != nil {
		t.Fatal(err)
	}
	if got, want := mustReadFile(t, hookOutput), filepath.Join(root, "todo", "hook-card.md")+"|todo|hook-card"; got != want {
		t.Fatalf("hook variables = %q, want %q", got, want)
	}

	if _, err := runSafeIngestCommand(t, "", "--board", root, "--name", "Safe card", "--content", "body"); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, hookOutput); !strings.Contains(got, "hook-card") || strings.Contains(got, "safe-card") {
		t.Fatalf("safe ingestion ran hook: %q", got)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
