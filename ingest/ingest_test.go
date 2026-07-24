package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kbrd/config"
	"kbrd/frontmatter"
)

func TestServiceCreateAppliesCanonicalCaptureMetadata(t *testing.T) {
	root := makeBoard(t, "1. TODO", "2. DOING")
	if err := os.WriteFile(filepath.Join(root, config.FolderConfigFile), []byte("[ingest]\ncreated_at_format = \"2006-01-02\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 24, 21, 15, 0, 0, time.FixedZone("CEST", 2*60*60))

	result, err := (Service{Now: func() time.Time { return now }}).Create(t.Context(), Request{
		Board: root, Column: "2", Name: "Shared / Page!", Content: "# Captured",
		Source: "chrome",
		Capture: &CaptureMetadata{
			SourceApp: "Google Chrome",
			URL:       "https://example.com/a?q=one&two=yes",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "shared-page" || result.Column != "2. DOING" {
		t.Fatalf("result = %+v", result)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	block, body, fenced := frontmatter.Split(string(raw))
	if !fenced || strings.TrimSpace(body) != "# Captured" {
		t.Fatalf("content = %q", raw)
	}
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"created_at":  "2026-07-24",
		"captured_at": "2026-07-24T19:15:00Z",
		"source":      "chrome",
		"source_app":  "Google Chrome",
		"url":         "https://example.com/a?q=one&two=yes",
	}
	for key, value := range want {
		if parsed.Data[key] != value {
			t.Errorf("%s = %#v, want %q", key, parsed.Data[key], value)
		}
	}
}

func TestServiceCreatePreservesExistingFrontmatterAndPlainIngestHasNoCaptureFields(t *testing.T) {
	root := makeBoard(t, "todo")
	now := time.Date(2026, time.July, 24, 19, 15, 0, 0, time.UTC)
	result, err := (Service{Now: func() time.Time { return now }}).Create(t.Context(), Request{
		Board: root, Name: "Existing", Source: "script",
		Content: "---\ntitle: Keep me\ncreated_at: old\n---\n\nbody",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	block, _, fenced := frontmatter.Split(string(raw))
	if !fenced {
		t.Fatal("missing frontmatter")
	}
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Data["title"] != "Keep me" || parsed.Data["created_at"] != "2026-07-24T19:15:00Z" || parsed.Data["source"] != "script" {
		t.Fatalf("frontmatter = %#v", parsed.Data)
	}
	if _, ok := parsed.Data["captured_at"]; ok {
		t.Fatalf("plain ingest unexpectedly has captured_at: %#v", parsed.Data)
	}
}

func TestServiceCreateReturnsHookWarningsAndSafeSkipsHooks(t *testing.T) {
	root := makeBoard(t, "todo")
	hookOutput := filepath.Join(root, "hook-output")
	hooks := "hooks:\n" +
		"  - name: Record created card\n" +
		"    id: record-created\n" +
		"    event: item_created\n" +
		"    command: printf created > '" + hookOutput + "'\n" +
		"  - name: Failing hook\n" +
		"    id: failing-hook\n" +
		"    event: item_created\n" +
		"    command: false\n"
	if err := os.WriteFile(filepath.Join(root, config.FolderHooksFile), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Create(t.Context(), Request{Board: root, Name: "Hooked", Content: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Source != "Failing hook" || result.Warnings[0].Message != "exited 1" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	if _, err := os.Stat(hookOutput); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hookOutput); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.Context(), Request{Board: root, Name: "Safe", Content: "body", Safe: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hookOutput); !os.IsNotExist(err) {
		t.Fatalf("safe ingestion ran hooks: %v", err)
	}
}

func TestServiceRejectsInvalidInputBeforeWriting(t *testing.T) {
	root := makeBoard(t, "todo")
	_, err := Create(t.Context(), Request{Board: root, Name: "Bad", Content: "body", Source: "bad\nsource"})
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("error = %v", err)
	}
	_, err = Create(t.Context(), Request{Board: root, Column: "2", Name: "Bad", Content: "body"})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "todo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func makeBoard(t *testing.T, columns ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := filepath.Join(t.TempDir(), "board")
	for _, column := range columns {
		if err := os.MkdirAll(filepath.Join(root, column), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
