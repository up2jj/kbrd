package web

import (
	"log"
	"os"
)

// defaultLogger preserves serve's stdout logging without changing the
// process-wide standard logger.
func defaultLogger(logger *log.Logger) *log.Logger {
	if logger != nil {
		return logger
	}
	return log.New(os.Stdout, "", log.LstdFlags)
}

func logf(logger *log.Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}
