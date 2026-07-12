package model

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// notifySeverity is deliberately internal: Go call sites use the descriptive
// Notifier methods, while scripting accepts the documented string levels.
type notifySeverity int

const (
	notifyInfo notifySeverity = iota
	notifySuccess
	notifyWarning
	notifyError
)

type notifyKind int

const (
	notifyNone notifyKind = iota
	notifyOSC99
	notifyOSC777
	notifyOSC9
	notifyOsascript
)

const maxNotificationBytes = 2000

type notifyDeps struct {
	getenv  func(string) string
	goos    string
	openTTY func() (io.WriteCloser, error)
	start   func(string, ...string) error
}

type Notifier struct {
	kind    notifyKind
	openTTY func() (io.WriteCloser, error)
	start   func(string, ...string) error
}

type notifyMsg struct {
	Message string
	Type    notifySeverity
}

func NewNotifier(backend string) *Notifier {
	return newNotifier(backend, notifyDeps{})
}

func newNotifier(backend string, deps notifyDeps) *Notifier {
	if deps.getenv == nil {
		deps.getenv = os.Getenv
	}
	if deps.goos == "" {
		deps.goos = runtime.GOOS
	}
	if deps.openTTY == nil {
		deps.openTTY = func() (io.WriteCloser, error) {
			return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		}
	}
	if deps.start == nil {
		deps.start = func(name string, args ...string) error {
			return exec.Command(name, args...).Start()
		}
	}
	return &Notifier{
		kind:    detectNotifyKindWith(backend, deps.getenv, deps.goos),
		openTTY: deps.openTTY,
		start:   deps.start,
	}
}

func detectNotifyKind(backend string) notifyKind {
	return detectNotifyKindWith(backend, os.Getenv, runtime.GOOS)
}

func detectNotifyKindWith(backend string, getenv func(string) string, goos string) notifyKind {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "osc99", "kitty":
		return notifyOSC99
	case "osc777":
		return notifyOSC777
	case "osc9":
		return notifyOSC9
	case "osascript":
		return notifyOsascript
	case "none", "off":
		return notifyNone
	}

	term := strings.ToLower(getenv("TERM"))
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	if getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || program == "kitty" {
		return notifyOSC99
	}
	if program == "wezterm" {
		return notifyOSC777
	}
	if program == "iterm.app" || program == "ghostty" || strings.Contains(term, "ghostty") {
		return notifyOSC9
	}
	if goos == "darwin" {
		return notifyOsascript
	}
	return notifyNone
}

func (n *Notifier) Send(message string, sev notifySeverity) tea.Cmd {
	return func() tea.Msg {
		n.fire(message, sev)
		return nil
	}
}

func (n *Notifier) Info(message string) tea.Cmd    { return n.Send(message, notifyInfo) }
func (n *Notifier) Success(message string) tea.Cmd { return n.Send(message, notifySuccess) }
func (n *Notifier) Warning(message string) tea.Cmd { return n.Send(message, notifyWarning) }
func (n *Notifier) Error(message string) tea.Cmd   { return n.Send(message, notifyError) }

func (n *Notifier) ErrorCause(prefix string, err error) tea.Cmd {
	if err == nil {
		return nil
	}
	if prefix == "" {
		return n.Error(err.Error())
	}
	return n.Error(prefix + ": " + err.Error())
}

func (n *Notifier) fire(message string, sev notifySeverity) {
	message = sanitizeNotificationText(message)
	if message == "" {
		return
	}
	title := "kbrd \u00b7 " + sevTitle(sev)

	switch n.kind {
	case notifyOSC99:
		n.writeTerminal(fmt.Sprintf("\x1b]99;i=1:d=0;%s\x1b\\\x1b]99;i=1:d=1:p=body:u=%d;%s\x1b\\", title, sevUrgency(sev), message))
	case notifyOSC777:
		n.writeTerminal(fmt.Sprintf("\x1b]777;notify;%s;%s\x1b\\", title, message))
	case notifyOSC9:
		n.writeTerminal(fmt.Sprintf("\x1b]9;%s: %s\x1b\\", title, message))
	case notifyOsascript:
		script := fmt.Sprintf("display notification %s with title %s", appleScriptString(message), appleScriptString(title))
		_ = n.start("osascript", "-e", script)
	}
}

func (n *Notifier) writeTerminal(sequence string) {
	tty, err := n.openTTY()
	if err != nil {
		return
	}
	defer tty.Close()
	_, _ = io.WriteString(tty, sequence)
}

func normalizeNotifySeverity(level string) notifySeverity {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "success":
		return notifySuccess
	case "warning", "warn":
		return notifyWarning
	case "error":
		return notifyError
	default:
		return notifyInfo
	}
}

func sevLabel(s notifySeverity) string {
	switch s {
	case notifySuccess:
		return "success"
	case notifyWarning:
		return "warning"
	case notifyError:
		return "error"
	default:
		return "info"
	}
}

func sevTitle(s notifySeverity) string {
	switch s {
	case notifySuccess:
		return "Success"
	case notifyWarning:
		return "Warning"
	case notifyError:
		return "Error"
	default:
		return "Info"
	}
}

func sevUrgency(s notifySeverity) int {
	switch s {
	case notifyInfo:
		return 0
	case notifyError:
		return 2
	default:
		return 1
	}
}

func sanitizeNotificationText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return truncateUTF8(strings.Join(strings.Fields(s), " "), maxNotificationBytes)
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		end = i
	}
	return s[:end]
}

func appleScriptString(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '"' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	out = append(out, '"')
	return string(out)
}
