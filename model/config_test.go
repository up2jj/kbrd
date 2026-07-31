package model

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"kbrd/config"

	"github.com/pelletier/go-toml/v2"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

func TestLocalConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got, err := localConfigPath()
	if err != nil {
		t.Fatalf("localConfigPath: %v", err)
	}
	want := filepath.Join(realPath(t, dir), config.FolderConfigFile)
	if realPath(t, filepath.Dir(got)) != filepath.Dir(want) || filepath.Base(got) != filepath.Base(want) {
		t.Fatalf("path: got %q want %q", got, want)
	}
}

func TestGlobalConfigPath_IsPure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := globalConfigPath()
	if err != nil {
		t.Fatalf("globalConfigPath: %v", err)
	}

	if filepath.Base(got) != config.GlobalConfigFile {
		t.Fatalf("filename: got %q want %q", filepath.Base(got), config.GlobalConfigFile)
	}
	if filepath.Base(filepath.Dir(got)) != config.AppDirName {
		t.Fatalf("parent dir name: got %q want %q", filepath.Base(filepath.Dir(got)), config.AppDirName)
	}

	if _, err := os.Stat(filepath.Dir(got)); !os.IsNotExist(err) {
		t.Fatalf("parent dir should not be created by path resolver; stat err: %v", err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("config file should not exist; stat err: %v", err)
	}
}

func TestEnsureConfigFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "x.toml")

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestConfigCommandEntries(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Pre-create the local config so we can assert Exists toggles.
	localPath := filepath.Join(cwd, config.FolderConfigFile)
	if err := os.WriteFile(localPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed local: %v", err)
	}

	entries := configCommandEntries()
	if len(entries) != 5 {
		t.Fatalf("entries: got %d want 5", len(entries))
	}

	local, global, localCmds, localMCP, localAgents := entries[0], entries[1], entries[2], entries[3], entries[4]

	if local.Key != "c" || global.Key != "C" || localCmds.Key != "x" || localMCP.Key != "m" || localAgents.Key != "a" {
		t.Fatalf("keys: got %q/%q/%q/%q/%q want c/C/x/m/a", local.Key, global.Key, localCmds.Key, localMCP.Key, localAgents.Key)
	}
	if filepath.Base(localMCP.Path) != config.FolderMCPFile {
		t.Fatalf("local mcp path basename: got %q", filepath.Base(localMCP.Path))
	}
	if filepath.Base(localAgents.Path) != config.FolderAgentsFile {
		t.Fatalf("local agents path basename: got %q", filepath.Base(localAgents.Path))
	}
	if local.Err != nil || global.Err != nil || localCmds.Err != nil || localMCP.Err != nil || localAgents.Err != nil {
		t.Fatalf("unexpected errors: local=%v global=%v localCmds=%v localMCP=%v localAgents=%v", local.Err, global.Err, localCmds.Err, localMCP.Err, localAgents.Err)
	}
	if filepath.Base(localCmds.Path) != config.FolderCommandsFile {
		t.Fatalf("local commands path basename: got %q", filepath.Base(localCmds.Path))
	}
	if filepath.Base(local.Path) != config.FolderConfigFile {
		t.Fatalf("local path basename: got %q", filepath.Base(local.Path))
	}
	if filepath.Base(global.Path) != config.GlobalConfigFile {
		t.Fatalf("global path basename: got %q", filepath.Base(global.Path))
	}
	if filepath.Base(filepath.Dir(global.Path)) != config.AppDirName {
		t.Fatalf("global parent dir name: got %q want %q",
			filepath.Base(filepath.Dir(global.Path)), config.AppDirName)
	}
	if !local.Exists {
		t.Fatal("local.Exists: got false, want true (file was seeded)")
	}
	if global.Exists {
		t.Fatal("global.Exists: got true, want false (no file written)")
	}
}

func TestEnsureMCPFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.FolderMCPFile)

	if err := ensureMCPFile(path, "127.0.0.1:9999"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"http://127.0.0.1:9999"`) {
		t.Fatalf("generated .mcp.json missing url: %s", data)
	}

	// Must not overwrite an existing file.
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureMCPFile(path, "127.0.0.1:1234"); err != nil {
		t.Fatalf("ensure existing: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "custom" {
		t.Fatalf("existing file was overwritten: %q", data)
	}
}

func TestConfigFileExists(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.toml")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !configFileExists(present) {
		t.Fatalf("expected true for existing file")
	}
	if configFileExists(filepath.Join(dir, "missing.toml")) {
		t.Fatalf("expected false for missing file")
	}
}

func TestEnsureConfigFile_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, config.Template) {
		t.Fatalf("contents: got %d bytes, want template (%d bytes)", len(got), len(config.Template))
	}
}

func TestEnsureConfigFile_PreservesExistingAndAddsCurrentExamples(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")
	sentinel := []byte("# user edits\n\n[display]\ncolumn_width = 41\n# theme = \"dark\" # my preferred example\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("ensureConfigFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(got)
	if !strings.Contains(text, "# user edits") || !strings.Contains(text, "column_width = 41") || !strings.Contains(text, `# theme = "dark" # my preferred example`) {
		t.Fatalf("user content was not preserved:\n%s", text)
	}
	if strings.Count(text, "column_width") != 1 || strings.Count(text, "# theme =") != 1 {
		t.Fatalf("existing active/commented options were duplicated:\n%s", text)
	}
	if !strings.Contains(text, "# card_view") || !strings.Contains(text, "[scripting]") || !strings.Contains(text, "# remote_require") {
		t.Fatalf("current examples were not added:\n%s", text)
	}
	if strings.Contains(text, "# id = \"start-work\"") {
		t.Fatalf("frontmatter preset fields were added as board options:\n%s", text)
	}

	before := append([]byte(nil), got...)
	if err := ensureConfigFile(path); err != nil {
		t.Fatalf("second ensureConfigFile: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second ensure: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("refreshing current examples is not idempotent")
	}
}

func TestMergeConfigExamples_DottedTableRemainsValid(t *testing.T) {
	content := []byte("git.diff_tool = \"git\"\n")
	updated, changed := mergeConfigExamples(content, config.Template)
	if !changed {
		t.Fatal("expected missing examples to be added")
	}
	if !strings.Contains(string(updated), "# git.sync_on_startup") {
		t.Fatalf("dotted suggestion missing:\n%s", updated)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(updated, &decoded); err != nil {
		t.Fatalf("updated config is invalid TOML: %v\n%s", err, updated)
	}

	refreshed, changed := mergeConfigExamples(updated, config.Template)
	if changed || !bytes.Equal(refreshed, updated) {
		t.Fatalf("refreshing dotted config is not idempotent: changed=%v\n%s", changed, refreshed)
	}
}

func TestMergeConfigExamples_MalformedFileIsUntouched(t *testing.T) {
	content := []byte("not = valid = toml\n")
	updated, changed := mergeConfigExamples(content, config.Template)
	if changed || !bytes.Equal(updated, content) {
		t.Fatalf("malformed config changed: changed=%v content=%q", changed, updated)
	}
}

func TestEnsureConfigFile_StatErrorBubblesUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based unreadable parent test does not behave portably on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}

	parent := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	if err := ensureConfigFile(filepath.Join(parent, "x.toml")); err == nil {
		t.Fatal("expected error from unreadable parent, got nil")
	}
}
