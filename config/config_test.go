package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_DefaultsOnly(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 32 || cfg.PreviewLines != 3 || cfg.Theme != "auto" || cfg.NotifyBackend != "auto" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Path != folder {
		t.Fatalf("path: got %q want %q", cfg.Path, folder)
	}
	if cfg.Ingest.CreatedAtFormat != time.RFC3339 {
		t.Fatalf("ingest.created_at_format: got %q want %q", cfg.Ingest.CreatedAtFormat, time.RFC3339)
	}
	if cfg.Reminders.Enabled || cfg.Reminders.List != "" || cfg.Reminders.InboxColumn != "Inbox" {
		t.Fatalf("unexpected reminders defaults: %+v", cfg.Reminders)
	}
	if cfg.Reminders.DeleteRemoteOnCardDelete {
		t.Fatal("remote deletion must default to disabled")
	}
	if got := strings.Join(cfg.Reminders.DoneColumns, ","); got != "Done" {
		t.Fatalf("reminders done columns: got %q want Done", got)
	}
}

func TestLoad_Reminders(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[reminders]
enabled = true
account = "iCloud"
list = "kbrd Work"
inbox_column = "1. TODO"
done_columns = ["3. DONE", "Archive"]
delete_remote_on_card_delete = true
`)

	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !cfg.Reminders.Enabled || cfg.Reminders.Account != "iCloud" || cfg.Reminders.List != "kbrd Work" {
		t.Fatalf("unexpected reminders config: %+v", cfg.Reminders)
	}
	if cfg.Reminders.InboxColumn != "1. TODO" || strings.Join(cfg.Reminders.DoneColumns, ",") != "3. DONE,Archive" {
		t.Fatalf("unexpected reminders columns: %+v", cfg.Reminders)
	}
	if !cfg.Reminders.DeleteRemoteOnCardDelete {
		t.Fatal("delete_remote_on_card_delete was not loaded")
	}
}

func TestLoad_IngestCreatedAtFormat(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[ingest]
created_at_format = "2006-01-02"
`)

	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Ingest.CreatedAtFormat != time.DateOnly {
		t.Fatalf("ingest.created_at_format: got %q want %q", cfg.Ingest.CreatedAtFormat, time.DateOnly)
	}
}

func TestLoad_BoardLocalFrontmatterPresets(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[[frontmatter_presets]]
id = "start-work"
name = "Start work"
columns = ["Doing"]
unset = ["blocked_by"]
set.status = "doing"
set.started_at = "{{now}}"
set.due_date = "{{today+1d}}"
set.tags = ["active", "{{column}}"]

[[frontmatter_presets]]
id = "done"
name = "Done"
set.status = "done"

[[frontmatter_presets]]
id = "review"
name = "Review"
columns = [1, "Doing"]
set.status = "review"
`)

	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if len(cfg.FrontmatterPresets) != 3 {
		t.Fatalf("presets: got %d want 3 (%+v)", len(cfg.FrontmatterPresets), cfg.FrontmatterPresets)
	}
	got := cfg.FrontmatterPresets[0]
	if got.ID != "start-work" || got.Name != "Start work" || len(got.Columns) != 1 || got.Columns[0] != "Doing" {
		t.Fatalf("first preset: %+v", got)
	}
	if got.Set["status"] != "doing" || got.Set["started_at"] != "{{now}}" || got.Set["due_date"] != "{{today+1d}}" {
		t.Fatalf("first preset set: %#v", got.Set)
	}
	if got.Set["tags"] == nil || len(got.Unset) != 1 || got.Unset[0] != "blocked_by" {
		t.Fatalf("first preset values: %+v", got)
	}
	selector := cfg.FrontmatterPresets[2]
	if len(selector.Columns) != 2 {
		t.Fatalf("mixed column selectors: %+v", selector.Columns)
	}
	if index, ok := presetColumnIndex(selector.Columns[0]); !ok || index != 1 {
		t.Fatalf("numeric column selector = (%d, %v), want (1, true)", index, ok)
	}
	if !selector.AppliesTo("Other", 1) || !selector.AppliesTo("Doing", 4) || selector.AppliesTo("Other", 2) {
		t.Fatalf("mixed selector matching is incorrect: %+v", selector.Columns)
	}
}

func TestLoad_FrontmatterPresetsAreBoardLocal(t *testing.T) {
	global := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(global, GlobalConfigFile), `
[[frontmatter_presets]]
id = "global"
name = "Global"
set.status = "global"
`)

	cfg, err := loadFrom(global, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if len(cfg.FrontmatterPresets) != 0 {
		t.Fatalf("global presets should not load into a board: %+v", cfg.FrontmatterPresets)
	}
}

func TestLoad_InvalidFrontmatterPreset(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[[frontmatter_presets]]
id = "bad"
name = "Bad"
set.status = "{{unknown}}"
`)

	_, err := loadFrom(t.TempDir(), folder)
	if err == nil || !strings.Contains(err.Error(), "unknown variable") {
		t.Fatalf("error = %v, want unknown-variable validation error", err)
	}
}

func TestLoad_InvalidFrontmatterPresetDateExpression(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[[frontmatter_presets]]
id = "bad-date"
name = "Bad date"
set.due = "{{today+1x}}"
`)

	_, err := loadFrom(t.TempDir(), folder)
	if err == nil || !strings.Contains(err.Error(), "invalid date expression") {
		t.Fatalf("error = %v, want invalid-date-expression error", err)
	}
}

func TestLoad_InvalidNumericFrontmatterColumn(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderConfigFile), `
[[frontmatter_presets]]
id = "bad"
name = "Bad"
columns = [0]
set.status = "todo"
`)

	_, err := loadFrom(t.TempDir(), folder)
	if err == nil || !strings.Contains(err.Error(), "positive 1-based") {
		t.Fatalf("error = %v, want invalid numeric-column error", err)
	}
}

func TestLoad_GlobalOnly(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
theme = "dark"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 50 {
		t.Fatalf("column_width: got %d want 50", cfg.ColumnWidth)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme: got %q want dark", cfg.Theme)
	}
	if cfg.PreviewLines != 3 {
		t.Fatalf("preview_lines: got %d want default 3", cfg.PreviewLines)
	}
}

func TestLoad_ThemeLightOverride(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
theme = "light"
`)

	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Theme != "light" {
		t.Fatalf("theme: got %q want light", cfg.Theme)
	}
}

func TestLoad_ThemeUnknownNormalizesToAuto(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
theme = "sepia"
`)

	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Theme != "auto" {
		t.Fatalf("unknown theme should fall back to auto, got %q", cfg.Theme)
	}
}

func TestLoad_PerFolderOverridesGlobal(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
column_width = 24
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 24 {
		t.Fatalf("column_width: got %d want 24", cfg.ColumnWidth)
	}
}

func TestLoad_PerFolderPartialMerge(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
column_width = 50
preview_lines = 10
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[display]
column_width = 24
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.ColumnWidth != 24 {
		t.Fatalf("column_width: got %d want 24", cfg.ColumnWidth)
	}
	if cfg.PreviewLines != 10 {
		t.Fatalf("preview_lines: got %d want 10 (inherited from global)", cfg.PreviewLines)
	}
}

func TestLoad_EnvBeatsConfig(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[notify]
backend = "osascript"
`)
	t.Setenv("KBRD_NOTIFY", "osc9")

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.NotifyBackend != "osc9" {
		t.Fatalf("notify backend: got %q want osc9", cfg.NotifyBackend)
	}
}

func TestLoad_MalformedTOML(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(folder, "kbrd.toml"), "not = valid = toml = nope")

	_, err := loadFrom(globalDir, folder)
	if err == nil {
		t.Fatal("expected error for malformed TOML, got nil")
	}
	if !strings.Contains(err.Error(), "folder config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTemplate_IsValidGlobalConfig(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, GlobalConfigFile), string(Template))

	if _, err := loadFrom(globalDir, folder); err != nil {
		t.Fatalf("Template is not a valid global config: %v", err)
	}
}

func TestLoad_AutoSyncInterval_Default(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 0 {
		t.Fatalf("default auto_sync_interval: got %v want 0", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_AutoSyncInterval_Parsed(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
auto_sync_interval = "5m"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 5*time.Minute {
		t.Fatalf("auto_sync_interval: got %v want 5m", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_AutoSyncInterval_Invalid(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
auto_sync_interval = "banana"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoSyncInterval != 0 {
		t.Fatalf("invalid auto_sync_interval should yield 0, got %v", cfg.GitAutoSyncInterval)
	}
}

func TestLoad_ManualSyncMode_DefaultAttended(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitManualSyncMode != "attended" {
		t.Fatalf("default manual_sync_mode: got %q want attended", cfg.GitManualSyncMode)
	}
}

func TestLoad_ManualSyncMode_Auto(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
manual_sync_mode = "auto"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitManualSyncMode != "auto" {
		t.Fatalf("manual_sync_mode: got %q want auto", cfg.GitManualSyncMode)
	}
}

func TestLoad_ManualSyncMode_UnknownNormalizes(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
manual_sync_mode = "banana"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitManualSyncMode != "attended" {
		t.Fatalf("unknown manual_sync_mode should fall back to attended, got %q", cfg.GitManualSyncMode)
	}
}

func TestLoad_SyncOnStartup_DefaultTrue(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !cfg.GitSyncOnStartup {
		t.Fatal("sync_on_startup should default to true")
	}
}

func TestLoad_SyncOnStartup_Disabled(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
sync_on_startup = false
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitSyncOnStartup {
		t.Fatal("sync_on_startup = false should disable startup sync")
	}
}

func TestLoad_AutoCommit_DefaultFalse(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.GitAutoCommit {
		t.Fatal("auto_commit should default to false")
	}
}

func TestLoad_AutoCommit_Enabled(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[git]
auto_commit = true
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !cfg.GitAutoCommit {
		t.Fatal("auto_commit = true should enable auto-commit")
	}
}

func TestLoad_BoardItemDoubleClick_DefaultPeek(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.BoardItemDoubleClick != "peek" {
		t.Fatalf("default board.item_double_click: got %q want peek", cfg.BoardItemDoubleClick)
	}
}

func TestLoad_BoardItemDoubleClick_Edit(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[board]
item_double_click = "edit"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.BoardItemDoubleClick != "edit" {
		t.Fatalf("board.item_double_click: got %q want edit", cfg.BoardItemDoubleClick)
	}
}

func TestLoad_BoardItemDoubleClick_UnknownNormalizes(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[board]
item_double_click = "external"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.BoardItemDoubleClick != "peek" {
		t.Fatalf("unknown board.item_double_click should fall back to peek, got %q", cfg.BoardItemDoubleClick)
	}
}

func TestLoad_MissingFolderPresentGlobal(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[display]
theme = "dark"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme: got %q want dark", cfg.Theme)
	}
}

func TestLoad_ServeSection(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	globalDir := t.TempDir()
	folder := t.TempDir()

	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[serve]
addr = ":9090"
pull_interval = "30s"
`)
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[serve]
pull_interval = "5s"
`)

	cfg, err := loadFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Serve.Addr != ":9090" {
		t.Fatalf("serve.addr: got %q want :9090 (inherited from global)", cfg.Serve.Addr)
	}
	if cfg.Serve.PullInterval != "5s" {
		t.Fatalf("serve.pull_interval: got %q want 5s (folder overrides global)", cfg.Serve.PullInterval)
	}
	if cfg.Serve.Domain != "" {
		t.Fatalf("serve.domain: got %q want empty default", cfg.Serve.Domain)
	}
	if cfg.Serve.TokenInTOML {
		t.Fatal("TokenInTOML: got true without serve.token present")
	}
}

func TestLoad_ServeDefaultsEmpty(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	cfg, err := loadFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if cfg.Serve.Addr != "" || cfg.Serve.Domain != "" || cfg.Serve.PullInterval != "" {
		t.Fatalf("serve defaults must be empty (unset), got %+v", cfg.Serve)
	}
}

func TestLoad_ServeTokenDetectedNotRead(t *testing.T) {
	t.Setenv("KBRD_NOTIFY", "")
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "kbrd.toml"), `
[serve]
token = "supersecretvalue"
`)
	cfg, err := loadFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if !cfg.Serve.TokenInTOML {
		t.Fatal("TokenInTOML: got false, want true")
	}
}

func TestValidateServe(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr string // substring; "" means valid
	}{
		{"empty", "", ""},
		{"no serve section", "[board]\nname = \"x\"\n", ""},
		{"valid serve", "[serve]\naddr = \":9090\"\npull_interval = \"30s\"\n", ""},
		{"zero interval", "[serve]\npull_interval = \"0\"\n", ""},
		{"bad toml", "not = valid = toml", "invalid TOML"},
		{"token present", "[serve]\ntoken = \"abc\"\n", "serve.token cannot be set"},
		{"bad interval", "[serve]\npull_interval = \"banana\"\n", "not a duration"},
		{"negative interval", "[serve]\npull_interval = \"-5s\"\n", "must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateServe([]byte(tc.toml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateServe: unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateServe: got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
