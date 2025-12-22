package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LogLevel represents the available logging levels
type LogLevel string

const (
	LogLevelError LogLevel = "error"
	LogLevelWarn  LogLevel = "warn"
	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
)

// parseLogLevel converts a string to a LogLevel
func parseLogLevel(level string) (LogLevel, error) {
	switch strings.ToLower(level) {
	case "error":
		return LogLevelError, nil
	case "warn", "warning":
		return LogLevelWarn, nil
	case "info":
		return LogLevelInfo, nil
	case "debug":
		return LogLevelDebug, nil
	default:
		return "", fmt.Errorf("invalid log level: %s (must be error, warn, info, or debug)", level)
	}
}

// setupLogger creates and configures a slog logger based on log level
func setupLogger(level LogLevel) *slog.Logger {
	var slogLevel slog.Level

	switch level {
	case LogLevelError:
		slogLevel = slog.LevelError
	case LogLevelWarn:
		slogLevel = slog.LevelWarn
	case LogLevelInfo:
		slogLevel = slog.LevelInfo
	case LogLevelDebug:
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}
