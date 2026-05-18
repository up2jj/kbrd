package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileLogger writes script log entries to ~/.cache/kbrd/script.log.
// It opens the file lazily on first write and is safe for concurrent use.
type FileLogger struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

// NewFileLogger returns a logger writing to ~/.cache/kbrd/script.log.
// Failures to open the file are deferred — Log becomes a no-op rather than
// crashing the host.
func NewFileLogger() *FileLogger {
	cache, err := os.UserCacheDir()
	if err != nil {
		return &FileLogger{}
	}
	return &FileLogger{path: filepath.Join(cache, "kbrd", "script.log")}
}

func (l *FileLogger) Log(level, source, msg string) {
	if l == nil || l.path == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return
		}
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		l.f = f
	}
	fmt.Fprintf(l.f, "%s [%s] %s: %s\n",
		time.Now().UTC().Format(time.RFC3339), level, source, msg)
}

// Close releases the underlying file. Safe to call multiple times.
func (l *FileLogger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}
