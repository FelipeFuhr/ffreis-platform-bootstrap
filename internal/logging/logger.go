package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New constructs a structured logger. It writes to stderr so that stdout
// remains available for machine-readable output (e.g., printed credentials).
//
// Format selection:
//   - JSON when json=true or when stderr is not a TTY (CI environments).
//   - Text (human-readable) when stderr is an interactive terminal.
//
// Level is one of: debug, info, warn, error (case-insensitive).
// Unrecognised values fall back to info.
func New(level string, json bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
		// Include source file and line in debug output only.
		AddSource: strings.ToLower(level) == "debug",
	}

	if json || !IsTTY() {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// IsTTY reports whether stderr is an interactive terminal.
// Returns false in CI environments, pipes, and redirected output.
func IsTTY() bool {
	stat, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// parseLevel converts a level string to slog.Level.
// Unrecognised values return slog.LevelInfo so the logger always works.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
