package tors

import (
	"log/slog"
	"os"
	"strings"
)

func newLogger(l *Logging) *slog.Logger {
	level := slog.LevelInfo
	format := "text"
	if l != nil {
		switch strings.ToLower(l.Level) {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
		if l.Format != "" {
			format = l.Format
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
