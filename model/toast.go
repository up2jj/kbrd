package model

import (
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toastEventType int

const (
	toastSuccess toastEventType = iota
	toastError
)

type Toast struct {
	Message string
	Color   string
	Type    toastEventType
	Progress float64
}

type ToastManager struct {
	toasts []Toast
	max    int
}

func NewToastManager() *ToastManager {
	return &ToastManager{
		toasts: make([]Toast, 0),
		max:    3,
	}
}

func (tm *ToastManager) Add(message string, eventType toastEventType) tea.Cmd {
	if len(tm.toasts) > 0 {
		tm.toasts[0].Progress = 0
	}

	toast := Toast{
		Message:  message,
		Type:     eventType,
		Progress: 1.0,
	}
	if toast.Type == toastSuccess {
		toast.Color = "green"
	} else {
		toast.Color = "red"
	}

	tm.toasts = append([]Toast{toast}, tm.toasts...)
	if len(tm.toasts) > tm.max {
		tm.toasts = tm.toasts[:tm.max]
	}

	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return toastTickMsg{}
	})
}

func (tm *ToastManager) Update(msg tea.Msg) (*ToastManager, tea.Cmd) {
	switch msg.(type) {
	case toastTickMsg:
		tm.toasts = removeExpiredToasts(tm.toasts)
		if len(tm.toasts) > 0 {
			return tm, tea.Batch(tm.tickToasts()...)
		}
		return tm, nil
	}
	return tm, nil
}

func (tm *ToastManager) tickToasts() []tea.Cmd {
	cmds := make([]tea.Cmd, 0)
	for i := range tm.toasts {
		tm.toasts[i].Progress -= 0.05
		if tm.toasts[i].Progress > 0 {
			cmds = append(cmds, tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
				return toastTickMsg{}
			}))
		}
	}
	return cmds
}

func removeExpiredToasts(toasts []Toast) []Toast {
	result := []Toast{}
	for _, t := range toasts {
		if t.Progress > 0 {
			result = append(result, t)
		}
	}
	return result
}

func (tm *ToastManager) Render() string {
	if len(tm.toasts) == 0 {
		return ""
	}

	style := lipgloss.NewStyle().
		Padding(0, 1).
		MaxWidth(60)

	var result string
	for i, t := range tm.toasts {
		msg := t.Message
		if t.Type == toastSuccess {
			msg = "✓ " + msg
		} else {
			msg = "✗ " + msg
		}

		s := style.Copy()
		if t.Color == "green" {
			s = s.Foreground(lipgloss.Color("#4ade80"))
		} else {
			s = s.Foreground(lipgloss.Color("#f87171"))
		}

		rendered := s.Render(msg)
		if i > 0 {
			result += "\n"
		}
		result += rendered
	}
	return result
}

type toastTickMsg struct{}

type toastMsg struct {
	Message string
	Type    toastEventType
}
