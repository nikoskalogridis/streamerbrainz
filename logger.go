package main

import (
	"log/slog"
	"os"
)

// setupLogger creates and configures a slog logger based on verbose flag
func setupLogger(verbose bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if verbose {
		opts.Level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}
