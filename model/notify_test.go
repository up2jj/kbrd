package model

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"kbrd/config"
)

type testTTY struct {
	bytes.Buffer
	closed bool
}

func (t *testTTY) Close() error {
	t.closed = true
	return nil
}

func TestDetectNotifyKind(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		env     map[string]string
		goos    string
		want    notifyKind
	}{
		{name: "explicit osc99", backend: "osc99", want: notifyOSC99},
		{name: "kitty alias", backend: "kitty", want: notifyOSC99},
		{name: "explicit disabled", backend: "off", want: notifyNone},
		{name: "kitty environment wins", backend: "auto", env: map[string]string{"KITTY_WINDOW_ID": "1", "TERM_PROGRAM": "WezTerm"}, want: notifyOSC99},
		{name: "kitty term", backend: "auto", env: map[string]string{"TERM": "xterm-kitty"}, want: notifyOSC99},
		{name: "wezterm", backend: "auto", env: map[string]string{"TERM_PROGRAM": "WezTerm"}, want: notifyOSC777},
		{name: "iterm", backend: "auto", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: notifyOSC9},
		{name: "ghostty", backend: "auto", env: map[string]string{"TERM": "xterm-ghostty"}, want: notifyOSC9},
		{name: "mac fallback", backend: "auto", goos: "darwin", want: notifyCenter},
		{name: "unsupported terminal", backend: "auto", goos: "linux", want: notifyNone},
		{name: "unknown backend retains auto behavior", backend: "future", env: map[string]string{"TERM_PROGRAM": "Ghostty"}, want: notifyOSC9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := detectNotifyKindWith(tt.backend, getenv, tt.goos); got != tt.want {
				t.Fatalf("detectNotifyKindWith() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotifierTerminalProtocols(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		level   notifySeverity
		want    string
	}{
		{
			name:    "osc99 warning",
			backend: "osc99",
			level:   notifyWarning,
			want:    "\x1b]99;i=1:d=0;kbrd \u00b7 Warning\x1b\\\x1b]99;i=1:d=1:p=body:u=1;hello\x1b\\",
		},
		{
			name:    "osc777 error",
			backend: "osc777",
			level:   notifyError,
			want:    "\x1b]777;notify;kbrd \u00b7 Error;hello\x1b\\",
		},
		{
			name:    "osc9 info",
			backend: "osc9",
			level:   notifyInfo,
			want:    "\x1b]9;kbrd \u00b7 Info: hello\x1b\\",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tty := &testTTY{}
			n := newNotifier(tt.backend, notifyDeps{
				openTTY: func() (io.WriteCloser, error) { return tty, nil },
			})
			n.fire("hello", tt.level)
			if got := tty.String(); got != tt.want {
				t.Fatalf("terminal output = %q, want %q", got, tt.want)
			}
			if !tty.closed {
				t.Fatal("terminal was not closed after the notification")
			}
		})
	}
}

func TestNotifierSanitizesAndTruncatesMessages(t *testing.T) {
	got := sanitizeNotificationText(" hello\n\x1b]unsafe\tworld\a ")
	if got != "hello ]unsafe world" {
		t.Fatalf("sanitized message = %q", got)
	}

	truncated := sanitizeNotificationText(strings.Repeat("\u4e16", 1000))
	if len(truncated) > maxNotificationBytes {
		t.Fatalf("truncated message is %d bytes, want at most %d", len(truncated), maxNotificationBytes)
	}
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated message is invalid UTF-8: %q", truncated)
	}
}

func TestNotifierDisabledDoesNotOpenTTY(t *testing.T) {
	opened := false
	n := newNotifier("none", notifyDeps{
		openTTY: func() (io.WriteCloser, error) {
			opened = true
			return &testTTY{}, nil
		},
	})
	n.fire("hello", notifyInfo)
	if opened {
		t.Fatal("disabled notifier opened the terminal")
	}
}

func TestNotifierNotificationCenterFallback(t *testing.T) {
	var gotName string
	var gotArgs []string
	n := newNotifier("notification-center", notifyDeps{
		start: func(name string, args ...string) error {
			gotName = name
			gotArgs = args
			return nil
		},
	})
	n.fire("hello\nworld", notifyError)
	if gotName != "/usr/bin/open" {
		t.Fatalf("command = %q, want /usr/bin/open", gotName)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "-b" || gotArgs[1] != "dev.kbrd.companion" {
		t.Fatalf("arguments = %q", gotArgs)
	}
	if !strings.Contains(gotArgs[2], "kbrd-notify://deliver?") || !strings.Contains(gotArgs[2], "message=hello+world") {
		t.Fatalf("notification URL = %q", gotArgs[2])
	}
}

func TestNotifierNotificationCenterCardActionsCarryRoute(t *testing.T) {
	var gotArgs []string
	n := newNotifier("notification-center", notifyDeps{start: func(_ string, args ...string) error {
		gotArgs = args
		return nil
	}})
	n.SetContext("/board", "/tmp/kbrd.sock")
	cmd := n.Card("due now", notifyWarning, "/board/Todo/card.md")
	_ = cmd()
	if len(gotArgs) != 3 {
		t.Fatalf("arguments = %q", gotArgs)
	}
	for _, want := range []string{"board=%2Fboard", "card=%2Fboard%2FTodo%2Fcard.md", "route=%2Ftmp%2Fkbrd.sock"} {
		if !strings.Contains(gotArgs[2], want) {
			t.Errorf("notification URL %q does not contain %q", gotArgs[2], want)
		}
	}
}

func TestNotifierRouteSurvivesBackendReload(t *testing.T) {
	b := NewBoardWithOptions(config.Config{
		Path:          "/board",
		NotifyBackend: "osc9",
	}, BoardOptions{NotificationRoute: "/tmp/kbrd.sock"})

	b.lifecycle().applyReloadedConfig(config.Config{
		Path:          "/board",
		NotifyBackend: "notification-center",
	})

	b.notifier.contextMu.RLock()
	boardPath, routePath := b.notifier.boardPath, b.notifier.routePath
	b.notifier.contextMu.RUnlock()
	if boardPath != "/board" || routePath != "/tmp/kbrd.sock" {
		t.Fatalf("notification context = (%q, %q)", boardPath, routePath)
	}
}

func TestScriptNotificationDoesNotInferCardActionsFromSelection(t *testing.T) {
	var gotArgs []string
	n := newNotifier("notification-center", notifyDeps{start: func(_ string, args ...string) error {
		gotArgs = args
		return nil
	}})
	n.SetContext("/board", "/tmp/kbrd.sock")
	col := NewColumn("Todo", "/board/Todo", ItemOptions{})
	col.SetItems([]Item{{
		Name:     "task",
		FullPath: "/board/Todo/task.md",
		Data:     map[string]any{"due": "tomorrow"},
	}})
	b := &Board{notifier: n, columns: []*Column{col}}

	(boardScriptAPI{b: b}).Notify("timer completed", "success")

	if len(gotArgs) != 3 {
		t.Fatalf("arguments = %q", gotArgs)
	}
	notificationURL, err := url.Parse(gotArgs[2])
	if err != nil {
		t.Fatal(err)
	}
	if cardPath := notificationURL.Query().Get("card"); cardPath != "" {
		t.Fatalf("generic script notification unexpectedly carries card actions: %q", gotArgs[2])
	}
}

func TestNotifierMethodsAndScriptLevels(t *testing.T) {
	tty := &testTTY{}
	n := newNotifier("osc9", notifyDeps{
		openTTY: func() (io.WriteCloser, error) { return tty, nil },
	})
	if cmd := n.Warning("careful"); cmd == nil {
		t.Fatal("Warning returned nil command")
	} else {
		_ = cmd()
	}
	if got := tty.String(); got != "\x1b]9;kbrd \u00b7 Warning: careful\x1b\\" {
		t.Fatalf("warning output = %q", got)
	}

	for level, wantTitle := range map[string]string{
		"info":    "Info",
		"success": "Success",
		"warning": "Warning",
		"error":   "Error",
		"unknown": "Info",
	} {
		tty = &testTTY{}
		n = newNotifier("osc9", notifyDeps{openTTY: func() (io.WriteCloser, error) { return tty, nil }})
		api := boardScriptAPI{b: &Board{notifier: n}}
		api.Notify("from lua", level)
		if got := tty.String(); got != "\x1b]9;kbrd \u00b7 "+wantTitle+": from lua\x1b\\" {
			t.Fatalf("level %q output = %q", level, got)
		}
	}
}
