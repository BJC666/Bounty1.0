package log

import (
	"fmt"
	"os"
	"time"
)

// Logger provides simple structured logging with a prefix.
type Logger struct {
	prefix string
}

// New creates a new Logger with the given prefix.
func New(prefix string) *Logger { return &Logger{prefix: prefix} }

// Info logs an informational message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.log("INFO", format, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log("WARN", format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.log("ERROR", format, args...)
}

func (l *Logger) log(level, format string, args ...interface{}) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[%s] %s [%s] %s\n", ts, level, l.prefix, msg)
}
