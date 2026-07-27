package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/capture"
	"kbrd/frontmatter"
	"kbrd/ingest"
	"kbrd/scratchpad"
)

func TestCompanionHotKeyPrintsNativeSettings(t *testing.T) {
	isolateConfig(t)
	var output bytes.Buffer
	cmd := newCompanionHotKeyCmd()
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"key_code":40`, `"modifiers":768`, `"label":"Command-Shift-K"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestCompanionScratchpadReadsTextFromStdin(t *testing.T) {
	isolateConfig(t)
	boardPath := makeIngestBoard(t, "todo")
	cmd := newCompanionScratchpadCmd()
	cmd.SetArgs([]string{"--board", boardPath})
	cmd.SetIn(strings.NewReader("private note"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	store, err := scratchpad.Open("")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "private note" {
		t.Fatalf("scratchpad = %q, want %q", got, "private note")
	}
}

func TestCompanionCaptureUsesCanonicalIngestion(t *testing.T) {
	isolateConfig(t)
	boardPath := makeIngestBoard(t, "todo")
	request, err := json.Marshal(companionCaptureInput{
		Board: boardPath, Column: "todo", Name: "Quick / Note", Content: "body",
		SourceApp: "Notes", URL: "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := newCompanionCaptureCmd()
	cmd.SetIn(bytes.NewReader(request))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result ingest.Result
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "quick-note" || result.Column != "todo" {
		t.Fatalf("result = %+v", result)
	}
	raw, err := os.ReadFile(filepath.Join(boardPath, "todo", "quick-note.md"))
	if err != nil {
		t.Fatal(err)
	}
	block, body, fenced := frontmatter.Split(string(raw))
	if !fenced || strings.TrimSpace(body) != "body" {
		t.Fatalf("content = %q", raw)
	}
	parsed, err := frontmatter.Parse([]byte(block))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Data["source"] != "companion" || parsed.Data["source_app"] != "Notes" || parsed.Data["url"] != "https://example.com" {
		t.Fatalf("frontmatter = %#v", parsed.Data)
	}
	if parsed.Data["created_at"] == nil || parsed.Data["captured_at"] == nil {
		t.Fatalf("timestamps missing from %#v", parsed.Data)
	}
}

func TestCompanionServiceCaptureRecordsDistinctSource(t *testing.T) {
	isolateConfig(t)
	boardPath := makeIngestBoard(t, "todo")
	request, err := json.Marshal(companionCaptureInput{
		Board: boardPath, Column: "todo", Name: "Service Note", Content: "body",
		Source: "macos-service", SourceApp: "Safari",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := newCompanionCaptureCmd()
	cmd.SetIn(bytes.NewReader(request))
	cmd.SetOut(new(bytes.Buffer))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(boardPath, "todo", "service-note.md"))
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
	if parsed.Data["source"] != "macos-service" || parsed.Data["source_app"] != "Safari" {
		t.Fatalf("frontmatter = %#v", parsed.Data)
	}
}

func TestCompanionPrepareCaptureConvertsStructuredInput(t *testing.T) {
	request, err := json.Marshal(capture.Input{
		HTML: `<h1>Shared</h1><p>A <strong>formatted</strong> note.</p>`,
		URL:  "https://example.com", SourceApp: "Safari",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := newCompanionPrepareCaptureCmd()
	cmd.SetIn(bytes.NewReader(request))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var prepared capture.Prepared
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Title != "Shared" || prepared.Markdown != "# Shared\n\nA **formatted** note." || prepared.URL != "https://example.com" || prepared.SourceApp != "Safari" {
		t.Fatalf("prepared = %+v", prepared)
	}
}
