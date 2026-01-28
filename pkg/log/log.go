// Package log provides a simple leveled logging package
//
// Log levels from least to most verbose:
//   - Error: Always printed, critical errors
//   - Warn: Warnings that don't stop execution
//   - Info: General informational messages (default)
//   - Debug: Detailed debugging information
//
// Usage:
//
//	log.SetLevel(log.LevelDebug)
//	log.Info("Starting operation on %s", target)
//	log.Debug("Detailed info: %v", data)
//	log.Error("Failed: %v", err)
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Level represents the logging level
type Level int

const (
	// LevelError is the least verbose level, only errors are printed
	LevelError Level = iota
	// LevelWarn prints warnings and errors
	LevelWarn
	// LevelInfo prints informational messages, warnings, and errors (default)
	LevelInfo
	// LevelDebug is the most verbose level, prints everything
	LevelDebug
)

// String returns the string representation of the log level
func (level Level) String() string {
	switch level {
	case LevelError:
		return "error"
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "debug"
	default:
		return "unknown"
	}
}

// ParseLevel parses a string into a Level.
// Returns LevelInfo if the string is not recognized.
func ParseLevel(levelStr string) Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "error", "err":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "info":
		return LevelInfo
	case "debug", "dbg":
		return LevelDebug
	default:
		return LevelInfo
	}
}

// Logger is a simple leveled logger
type Logger struct {
	mu     sync.RWMutex
	level  Level
	output io.Writer
}

// New creates a new Logger with the specified level and output writer
func New(level Level, output io.Writer) *Logger {
	return &Logger{
		level:  level,
		output: output,
	}
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// SetOutput sets the output writer
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// log writes a message if the message level is >= the logger's level
func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mu.RLock()
	currentLevel := l.level
	output := l.output
	l.mu.RUnlock()

	if level > currentLevel {
		return
	}

	msg := fmt.Sprintf(format, args...)
	// Ensure message ends with newline
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	fmt.Fprint(output, msg)
}

// Error logs an error message (always printed unless output is nil)
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Info logs an informational message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Global default logger that prints to os.Stdout
var std = New(LevelInfo, os.Stdout)

// SetLevel sets the level of the default logger
func SetLevel(level Level) {
	std.SetLevel(level)
}

// GetLevel returns the level of the default logger
func GetLevel() Level {
	return std.GetLevel()
}

// SetOutput sets the output of the default logger
func SetOutput(w io.Writer) {
	std.SetOutput(w)
}

// Error logs an error message using the default logger
func Error(format string, args ...interface{}) {
	std.Error(format, args...)
}

// Warn logs a warning message using the default logger
func Warn(format string, args ...interface{}) {
	std.Warn(format, args...)
}

// Info logs an informational message using the default logger
func Info(format string, args ...interface{}) {
	std.Info(format, args...)
}

// Debug logs a debug message using the default logger
func Debug(format string, args ...interface{}) {
	std.Debug(format, args...)
}

// ValidLevels returns a slice of valid level strings for help text
func ValidLevels() []string {
	return []string{"error", "warn", "info", "debug"}
}
