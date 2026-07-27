package capture

import (
	"strings"
	"testing"
)

func TestPrepareHTMLCapture(t *testing.T) {
	prepared, err := Prepare(Input{
		Title: "  Example article  ", SourceApp: " Safari ", URL: "https://example.com/post",
		HTML: `<article><h1>Heading</h1><p>Hello <strong>world</strong> and <a href="https://go.dev">Go</a>.</p><ul><li>One</li><li>Two</li></ul><blockquote>Quoted</blockquote><pre><code>go test ./...</code></pre></article>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Title != "Example article" || prepared.SourceApp != "Safari" || prepared.URL != "https://example.com/post" {
		t.Fatalf("prepared = %+v", prepared)
	}
	for _, want := range []string{
		"# Heading", "Hello **world** and [Go](https://go.dev).", "- One\n- Two", "> Quoted", "```\ngo test ./...\n```",
	} {
		if !strings.Contains(prepared.Markdown, want) {
			t.Errorf("markdown = %q, want %q", prepared.Markdown, want)
		}
	}
}

func TestPrepareFallsBackAcrossRepresentations(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  Prepared
	}{
		{
			name:  "plain text",
			input: Input{PlainText: "First line\r\nSecond line", SourceApp: "Notes"},
			want:  Prepared{Title: "First line", Markdown: "First line\nSecond line", SourceApp: "Notes"},
		},
		{
			name:  "url only",
			input: Input{URL: "https://www.example.com/path", Title: "Example"},
			want:  Prepared{Title: "Example", Markdown: "[Example](https://www.example.com/path)", URL: "https://www.example.com/path"},
		},
		{
			name:  "url derives host title",
			input: Input{URL: "https://www.example.com/path"},
			want:  Prepared{Title: "example.com", Markdown: "[example.com](https://www.example.com/path)", URL: "https://www.example.com/path"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Prepare(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Prepare() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPrepareRejectsEmptyAndOversizedCaptures(t *testing.T) {
	if _, err := Prepare(Input{}); err == nil || !strings.Contains(err.Error(), "no text") {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := Prepare(Input{PlainText: strings.Repeat("x", MaxInputBytes+1)}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestHTMLToMarkdownTable(t *testing.T) {
	got, err := HTMLToMarkdown(`<table><tr><th>Name</th><th>State</th></tr><tr><td>API</td><td>Ready</td></tr></table>`)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Name | State |\n| --- | --- |\n| API | Ready |"
	if got != want {
		t.Fatalf("HTMLToMarkdown() = %q, want %q", got, want)
	}
}
