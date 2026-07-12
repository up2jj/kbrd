package model

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"kbrd/config"
)

func TestReleaseCheckerChecksLatestStableRelease(t *testing.T) {
	tagName := "v1.2.0"
	htmlURL := "https://github.com/up2jj/kbrd/releases/tag/v1.2.0"
	requests := 0
	wantUserAgent := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got, want := r.URL.Path, "/repos/up2jj/kbrd/releases/latest"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/vnd.github+json"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("User-Agent"), wantUserAgent; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, `{"tag_name":"`+tagName+`","html_url":"`+htmlURL+`"}`)
	}))
	defer server.Close()

	checker := releaseChecker{client: server.Client(), endpoint: server.URL + "/repos/up2jj/kbrd/releases/latest", timeout: time.Second}
	tests := []struct {
		name    string
		local   string
		want    string
		wantURL string
	}{
		{name: "newer release", local: "v1.0.0", want: "v1.2.0", wantURL: htmlURL},
		{name: "same release", local: "v1.2.0"},
		{name: "newer local build", local: "v1.3.0"},
		{name: "without v prefix", local: "1.0.0", want: "v1.2.0", wantURL: htmlURL},
		{name: "development build", local: "dev"},
		{name: "malformed local version", local: "not-a-version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantUserAgent = "kbrd/" + normalizeReleaseVersion(tt.local) + " update-check"
			got := checker.check(tt.local)
			if got.version != tt.want || got.url != tt.wantURL {
				t.Fatalf("check(%q) = %#v, want version %q and URL %q", tt.local, got, tt.want, tt.wantURL)
			}
		})
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want 4 valid-version requests", requests)
	}
}

func TestReleaseCheckerIgnoresBadResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "non-success", status: http.StatusTooManyRequests, body: `{}`},
		{name: "malformed JSON", status: http.StatusOK, body: `not json`},
		{name: "malformed tag", status: http.StatusOK, body: `{"tag_name":"newest","html_url":"https://example.test/release"}`},
		{name: "missing release URL", status: http.StatusOK, body: `{"tag_name":"v1.2.0"}`},
		{name: "oversized response", status: http.StatusOK, body: `{"tag_name":"v1.2.0","html_url":"https://example.test/release","extra":"` + strings.Repeat("x", releaseResponseLimit) + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			checker := releaseChecker{client: server.Client(), endpoint: server.URL, timeout: time.Second}
			if got := checker.check("v1.0.0"); got != (releaseCheckMsg{}) {
				t.Fatalf("check() = %#v, want no update", got)
			}
		})
	}
}

func TestReleaseCheckerIgnoresRequestErrors(t *testing.T) {
	deadlineSeen := false
	checker := releaseChecker{
		client: &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			_, deadlineSeen = r.Context().Deadline()
			return nil, context.DeadlineExceeded
		})},
		endpoint: "https://example.test/releases/latest",
		timeout:  time.Second,
	}
	if got := checker.check("v1.0.0"); got != (releaseCheckMsg{}) {
		t.Fatalf("check() = %#v, want no update", got)
	}
	if !deadlineSeen {
		t.Fatal("request context did not have a deadline")
	}
}

func TestReleaseCheckerCommandSkipsUnversionedBuilds(t *testing.T) {
	checker := newReleaseChecker()
	if cmd := checker.command("dev"); cmd != nil {
		t.Fatal("command for dev build should be nil")
	}
}

func TestBoardInitSchedulesReleaseCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v1.2.0","html_url":"https://example.test/release"}`)
	}))
	defer server.Close()

	originalVersion := Version
	Version = "v1.0.0"
	t.Cleanup(func() { Version = originalVersion })
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.releaseChecker = releaseChecker{client: server.Client(), endpoint: server.URL, timeout: time.Second}

	batch, ok := b.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init did not return a batch")
	}
	for _, cmd := range batch {
		if msg, ok := cmd().(releaseCheckMsg); ok {
			if msg.version != "v1.2.0" {
				t.Fatalf("release result = %#v", msg)
			}
			return
		}
	}
	t.Fatal("Init did not schedule a release check")
}

func TestBoardHandleReleaseCheck(t *testing.T) {
	tty := &testTTY{}
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.notifier = newNotifier("osc9", notifyDeps{openTTY: func() (io.WriteCloser, error) { return tty, nil }})

	cmd := b.handleReleaseCheck(releaseCheckMsg{
		version: "v1.2.0",
		url:     "https://github.com/up2jj/kbrd/releases/tag/v1.2.0",
	})
	if cmd == nil {
		t.Fatal("new release should notify")
	}
	_ = cmd()
	if !strings.Contains(tty.String(), "update available: v1.2.0") || !strings.Contains(tty.String(), "https://github.com/up2jj/kbrd/releases/tag/v1.2.0") {
		t.Fatalf("notification = %q", tty.String())
	}

	b.statusPresenter().updateBuiltinCells()
	assertBuiltinCellText(t, b, builtinCellReleaseUpdate, "↑ update v1.2.0")
	if got := b.cells.cells[builtinCellReleaseUpdate.id()].FG; got != string(b.palette.Success) {
		t.Fatalf("update FG = %q, want success %q", got, b.palette.Success)
	}

	if cmd := b.handleReleaseCheck(releaseCheckMsg{}); cmd != nil {
		t.Fatal("empty result should not notify")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
