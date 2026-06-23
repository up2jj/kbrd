package model

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type notifySeverity int

const (
	notifySuccess notifySeverity = iota
	notifyError
)

type notifyKind int

const (
	notifyNone notifyKind = iota
	notifyOSC777
	notifyOSC9
	notifyOsascript
)

type Notifier struct {
	kind notifyKind
	tty  io.Writer
}

type notifyMsg struct {
	Message string
	Type    notifySeverity
}

func NewNotifier(backend string) *Notifier {
	n := &Notifier{kind: detectNotifyKind(backend)}

	if n.kind == notifyOSC777 || n.kind == notifyOSC9 {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			n.kind = notifyNone
		} else {
			n.tty = f
		}
	}
	return n
}

func detectNotifyKind(backend string) notifyKind {
	switch strings.ToLower(backend) {
	case "osascript":
		return notifyOsascript
	case "osc9":
		return notifyOSC9
	case "osc777":
		return notifyOSC777
	case "none", "off":
		return notifyNone
	case "auto", "":
		// fall through to auto-detection
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "WezTerm":
		return notifyOSC777
	case "iTerm.app":
		return notifyOSC9
	}
	if runtime.GOOS == "darwin" {
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

func (n *Notifier) Success(message string) tea.Cmd {
	return n.Send(message, notifySuccess)
}

func (n *Notifier) Error(message string) tea.Cmd {
	return n.Send(message, notifyError)
}

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
	switch n.kind {
	case notifyOSC777:
		fmt.Fprintf(n.tty, "\x1b]777;notify;kbrd: %s;%s\x1b\\", sevLabel(sev), message)
	case notifyOSC9:
		fmt.Fprintf(n.tty, "\x1b]9;%s %s\x07", sevGlyph(sev), message)
	case notifyOsascript:
		script := fmt.Sprintf("display notification %s with title %s",
			appleScriptString(message),
			appleScriptString("kbrd: "+sevLabel(sev)))
		_ = exec.Command("osascript", "-e", script).Start()
	}
}

func sevLabel(s notifySeverity) string {
	if s == notifySuccess {
		return "success"
	}
	return "error"
}

func sevGlyph(s notifySeverity) string {
	if s == notifySuccess {
		return "✓"
	}
	return "✗"
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
