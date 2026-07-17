package model

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/script"
)

var (
	sourceLinePattern = regexp.MustCompile(`(?m)([^\n:]+\.lua):(\d+):`)
	parseLinePattern  = regexp.MustCompile(`line:(\d+)`)
)

type scriptStartupState struct {
	active    bool
	expanded  bool
	switching bool
	path      string
	line      int
	message   string
	traceback string
	output    []script.LogRecord
	editError string
}

type scriptEditorDoneMsg struct{ err error }

// boardScriptStartup owns the startup preflight and recovery screen. Keeping
// this flow here leaves Board responsible only for routing Bubble Tea messages.
type boardScriptStartup struct {
	board *Board
}

func (b *Board) scriptStartupFlow() boardScriptStartup {
	return boardScriptStartup{board: b}
}

func (s boardScriptStartup) handleStart() (tea.Model, tea.Cmd) {
	s.board.showScriptActivity()
	return s.board, func() tea.Msg { return scriptInitRunMsg{} }
}

func (s boardScriptStartup) handleRun() (tea.Model, tea.Cmd) {
	b := s.board
	wasSwitch := b.scriptStartup.switching
	if err := b.initRuntime(); err != nil {
		b.openScriptStartupFailure(err, wasSwitch)
		b.clearScriptActivity()
		return b, nil
	}
	b.scriptStartup = scriptStartupState{}
	b.clearScriptActivity()
	if wasSwitch {
		cmd, err := b.session().finishBoardLoad()
		if err != nil {
			return b, b.notifier.ErrorCause("failed to load board", err)
		}
		return b, cmd
	}
	return b, b.startupCmd()
}

func (s boardScriptStartup) handleEditorDone(msg scriptEditorDoneMsg) (tea.Model, tea.Cmd) {
	b := s.board
	if msg.err != nil && b.scriptStartup.active {
		b.scriptStartup.editError = msg.err.Error()
	}
	return b, nil
}

func (s boardScriptStartup) view() tea.View {
	view := tea.NewView(s.board.renderScriptStartup())
	view.AltScreen = true
	return view
}

func (b *Board) openScriptStartupFailure(err error, switching bool) {
	path := filepath.Join(b.cfg.Path, script.FolderInitFile)
	message, line := scriptErrorSummary(err, path)
	b.scriptStartup = scriptStartupState{
		active:    true,
		switching: switching,
		path:      path,
		line:      line,
		message:   message,
		traceback: err.Error(),
	}
	if b.scriptLogger != nil {
		for _, record := range b.scriptLogger.Records() {
			if record.Level == "debug" && strings.Contains(record.Source, script.FolderInitFile) {
				b.scriptStartup.output = append(b.scriptStartup.output, record)
			}
		}
	}
}

func scriptErrorSummary(err error, fallbackPath string) (string, int) {
	raw := err.Error()
	line := 0
	if match := sourceLinePattern.FindStringSubmatch(raw); len(match) == 3 {
		line, _ = strconv.Atoi(match[2])
	} else if match := parseLinePattern.FindStringSubmatch(raw); len(match) == 2 {
		line, _ = strconv.Atoi(match[1])
	}
	message := strings.TrimSpace(strings.Split(raw, "\n")[0])
	for _, prefix := range []string{script.FolderInitFile + ": ", fallbackPath + ": "} {
		message = strings.TrimPrefix(message, prefix)
	}
	if match := sourceLinePattern.FindString(message); match != "" {
		message = strings.TrimSpace(strings.TrimPrefix(message, match))
	}
	if message == "" {
		message = "script initialization failed"
	}
	return message, line
}

func (s boardScriptStartup) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := s.board
	switch msg.String() {
	case "q", "ctrl+c":
		b.Close()
		return b, tea.Quit
	case "enter":
		b.scriptStartup.expanded = !b.scriptStartup.expanded
		return b, nil
	case "r":
		b.scriptStartup.editError = ""
		return b, func() tea.Msg { return scriptInitRunMsg{} }
	case "e":
		path := b.scriptStartup.path
		cmd := scriptEditorCommand(b.cfg.Path, path)
		return b, tea.ExecProcess(cmd, func(err error) tea.Msg { return scriptEditorDoneMsg{err: err} })
	}
	return b, nil
}

func scriptEditorCommand(boardDir, path string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", `exec ${VISUAL:-${EDITOR:-vi}} "$1"`, "kbrd-editor", path)
	cmd.Dir = boardDir
	return cmd
}

func (b *Board) renderScriptStartup() string {
	s := b.scriptStartup
	width := 80
	if b.termWidth > 0 {
		width = min(max(b.termWidth-4, 24), 100)
	}
	bodyWidth := max(width-6, 20)
	muted := lipgloss.NewStyle().Foreground(b.palette.FgMuted)
	danger := lipgloss.NewStyle().Foreground(b.palette.Danger).Bold(true)
	lineNo := lipgloss.NewStyle().Foreground(b.palette.FgSubtle)
	highlight := lipgloss.NewStyle().Foreground(b.palette.Danger).Bold(true)

	var lines []string
	location := script.FolderInitFile
	if s.line > 0 {
		location += ":" + strconv.Itoa(s.line)
	}
	lines = append(lines, danger.Render("✕ "+location), s.message, "")

	if len(s.output) > 0 {
		lines = append(lines, muted.Render("Script output"))
		start := max(len(s.output)-8, 0)
		for _, record := range s.output[start:] {
			source := filepath.Base(strings.Split(record.Source, ":")[0])
			lines = append(lines, fmt.Sprintf("%s  %-5s  %-12s  %s",
				record.Time.Local().Format("15:04:05"), strings.ToUpper(record.Level), source, record.Message))
		}
		lines = append(lines, "")
	}

	if excerpt := scriptSourceExcerpt(s.path, s.line, lineNo, highlight); excerpt != "" {
		lines = append(lines, muted.Render("Source"), excerpt, "")
	}
	if s.expanded {
		lines = append(lines, muted.Render("Traceback"), s.traceback, "")
	}
	if s.editError != "" {
		lines = append(lines, danger.Render("editor: "+s.editError), "")
	}
	lines = append(lines, muted.Render("e edit in $EDITOR   r retry   enter traceback   q quit"))

	body := lipgloss.NewStyle().Width(bodyWidth).Render(strings.Join(lines, "\n"))
	frame := OverlayFrame{
		Title:   ".kbrd.lua startup failed",
		Body:    body,
		Palette: b.palette,
	}.Render()
	height := b.termHeight
	if height <= 0 {
		height = lipgloss.Height(frame)
	}
	termWidth := b.termWidth
	if termWidth <= 0 {
		termWidth = width
	}
	return lipgloss.Place(termWidth, height, lipgloss.Center, lipgloss.Center, frame)
}

func scriptSourceExcerpt(path string, target int, lineStyle, targetStyle lipgloss.Style) string {
	if target <= 0 {
		return ""
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	all := strings.Split(string(body), "\n")
	from := max(target-3, 1)
	to := min(target+2, len(all))
	out := make([]string, 0, to-from+1)
	for n := from; n <= to; n++ {
		marker := " "
		style := lineStyle
		if n == target {
			marker = "›"
			style = targetStyle
		}
		out = append(out, style.Render(fmt.Sprintf("%s %3d │ %s", marker, n, all[n-1])))
	}
	return strings.Join(out, "\n")
}
