package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxScriptLogSize  = 5 << 20
	maxScriptLogFiles = 3
	maxCapturedLogs   = 200
)

// LogRecord is one in-memory copy of a script log entry. FileLogger retains a
// small tail so startup failures can show output emitted before the error.
type LogRecord struct {
	Time    time.Time
	Level   string
	Source  string
	Message string
}

// FileLogger writes script log entries to ~/.cache/kbrd/script.log.
// It opens the file lazily on first write and is safe for concurrent use.
type FileLogger struct {
	mu      sync.Mutex
	path    string
	f       *os.File
	records []LogRecord
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
	now := time.Now().UTC()
	l.records = append(l.records, LogRecord{Time: now, Level: level, Source: source, Message: msg})
	if len(l.records) > maxCapturedLogs {
		l.records = append([]LogRecord(nil), l.records[len(l.records)-maxCapturedLogs:]...)
	}
	line := fmt.Sprintf("%s [%s] %s: %s\n", now.Format(time.RFC3339), level, source, msg)
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
	if info, err := l.f.Stat(); err == nil && info.Size()+int64(len(line)) > maxScriptLogSize {
		l.rotateLocked()
		if l.f == nil {
			f, openErr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if openErr != nil {
				return
			}
			l.f = f
		}
	}
	_, _ = l.f.WriteString(line)
}

// Records returns a copy of the recent in-memory log tail.
func (l *FileLogger) Records() []LogRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogRecord(nil), l.records...)
}

// rotateLocked rotates script.log through script.log.3. Rotation is best
// effort: failures leave the current file available whenever possible and are
// never allowed to affect board startup.
func (l *FileLogger) rotateLocked() {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, maxScriptLogFiles))
	for i := maxScriptLogFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.path, i)
		newPath := fmt.Sprintf("%s.%d", l.path, i+1)
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return
		}
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil && !os.IsNotExist(err) {
		// Reopen the original below so logging can continue.
		return
	}
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
