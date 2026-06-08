package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOverride writes content to <board>/.kbrd_web_templates/<rel>.
func writeOverride(t *testing.T, board, rel, content string) {
	t.Helper()
	p := filepath.Join(board, WebDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTemplatesNoOverrideFolder(t *testing.T) {
	tmpl, err := buildTemplates(t.TempDir())
	if err != nil {
		t.Fatalf("buildTemplates: %v", err)
	}
	if tmpl.Lookup("board.html") == nil {
		t.Fatal("embedded board.html not present")
	}
}

func TestBuildTemplatesOverrideShadows(t *testing.T) {
	board := t.TempDir()
	writeOverride(t, board, "templates/login.html", "OVERRIDDEN LOGIN")

	tmpl, err := buildTemplates(board)
	if err != nil {
		t.Fatalf("buildTemplates: %v", err)
	}
	var sb strings.Builder
	if err := tmpl.ExecuteTemplate(&sb, "login.html", nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := sb.String(); !strings.Contains(got, "OVERRIDDEN LOGIN") {
		t.Fatalf("override not applied, got %q", got)
	}
}

func TestBuildTemplatesBadOverrideErrors(t *testing.T) {
	board := t.TempDir()
	writeOverride(t, board, "templates/login.html", "{{ this is not valid")
	if _, err := buildTemplates(board); err == nil {
		t.Fatal("expected parse error for malformed override")
	}
}

func TestStaticFSOverrideAndFallback(t *testing.T) {
	board := t.TempDir()
	writeOverride(t, board, "static/style.css", "body{color:hotpink}")

	srv := httptest.NewServer(http.StripPrefix("/static/", http.FileServerFS(staticFS(board))))
	defer srv.Close()

	// Overridden file wins.
	css := mustGet(t, srv.URL+"/static/style.css")
	if !strings.Contains(css, "hotpink") {
		t.Fatalf("override css not served, got %q", css)
	}
	// Non-overridden file falls back to embedded.
	js := mustGet(t, srv.URL+"/static/htmx.min.js")
	if js == "" {
		t.Fatal("embedded htmx.min.js not served via fallback")
	}
}

func mustGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestEjectAssetsWritesAndNeverClobbers(t *testing.T) {
	board := t.TempDir()
	written, skipped, err := EjectAssets(board)
	if err != nil {
		t.Fatalf("eject: %v", err)
	}
	if len(written) == 0 || len(skipped) != 0 {
		t.Fatalf("first eject: written=%d skipped=%d", len(written), len(skipped))
	}
	// Files landed under the mirrored layout.
	if _, err := os.Stat(filepath.Join(board, WebDir, "templates", "board.html")); err != nil {
		t.Fatalf("board.html not written: %v", err)
	}

	// Edit one file, then re-eject: nothing is overwritten.
	edited := filepath.Join(board, WebDir, "templates", "board.html")
	if err := os.WriteFile(edited, []byte("MINE"), 0o644); err != nil {
		t.Fatal(err)
	}
	written2, skipped2, err := EjectAssets(board)
	if err != nil {
		t.Fatalf("re-eject: %v", err)
	}
	if len(written2) != 0 || len(skipped2) != len(written) {
		t.Fatalf("re-eject should skip all: written=%d skipped=%d (want 0/%d)", len(written2), len(skipped2), len(written))
	}
	if b, _ := os.ReadFile(edited); string(b) != "MINE" {
		t.Fatalf("re-eject clobbered an edited file: %q", b)
	}
}
