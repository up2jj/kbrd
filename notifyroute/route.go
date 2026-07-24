// Package notifyroute carries actions from the macOS notification companion
// back to the running TUI. The socket path is included in each notification,
// so no global process registry is required.
package notifyroute

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Action string

const (
	OpenCard  Action = "open-card"
	MarkDone  Action = "mark-done"
	SnoozeDue Action = "snooze-due"
	RetrySync Action = "retry-sync"
)

type Command struct {
	Action    Action `json:"action"`
	BoardPath string `json:"board_path,omitempty"`
	CardPath  string `json:"card_path,omitempty"`
	SyncKind  string `json:"sync_kind,omitempty"`
}

type Server struct {
	listener net.Listener
	path     string
	commands chan Command
	done     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
}

func Listen() (*Server, error) {
	dir, err := os.MkdirTemp("", "kbrd-notify-")
	if err != nil {
		return nil, fmt.Errorf("create notification route: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("secure notification route: %w", err)
	}
	path := filepath.Join(dir, "route.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for notification actions: %w", err)
	}
	s := &Server{listener: listener, path: path, commands: make(chan Command), done: make(chan struct{}), stop: make(chan struct{})}
	go s.serve()
	return s, nil
}

func (s *Server) Path() string             { return s.path }
func (s *Server) Commands() <-chan Command { return s.commands }

func (s *Server) Close() error {
	select {
	case <-s.done:
		return nil
	default:
	}
	s.stopOnce.Do(func() { close(s.stop) })
	err := s.listener.Close()
	<-s.done
	_ = os.RemoveAll(filepath.Dir(s.path))
	return err
}

func (s *Server) serve() {
	defer close(s.done)
	defer close(s.commands)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		var command Command
		err = json.NewDecoder(conn).Decode(&command)
		_ = conn.Close()
		if err != nil || !command.Valid() {
			continue
		}
		select {
		case s.commands <- command:
		case <-s.stop:
			return
		}
	}
}

func (c Command) Valid() bool {
	switch c.Action {
	case OpenCard, MarkDone, SnoozeDue:
		return c.BoardPath != "" && c.CardPath != ""
	case RetrySync:
		return c.BoardPath != "" && (c.SyncKind == "git" || c.SyncKind == "reminders")
	default:
		return false
	}
}

func Send(socketPath string, command Command) error {
	if socketPath == "" || !command.Valid() {
		return fmt.Errorf("invalid notification action")
	}
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return fmt.Errorf("connect to kbrd: %w", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(command); err != nil {
		return fmt.Errorf("send notification action: %w", err)
	}
	return nil
}
