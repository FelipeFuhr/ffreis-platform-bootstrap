package logging

import (
	"context"
	"log/slog"
)

// contextKey is an unexported type so our context value never collides
// with keys set by other packages.
type contextKey struct{}

// WithLogger stores l in ctx and returns the derived context.
// Call this in PersistentPreRunE so every downstream handler can retrieve
// the logger without importing the logging package directly.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the logger stored by WithLogger.
// If no logger is present it returns slog.Default(), which is always safe
// to call and avoids nil-pointer panics in tests or early-init paths.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
