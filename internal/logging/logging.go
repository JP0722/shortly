// Package logging builds the application's structured logger.
package logging

import (
	"log/slog"
	"os"
)

// New returns a logger configured for the given level and environment:
// human-readable text in development, JSON in production.
//
// The logger is returned rather than installed globally via slog.SetDefault.
// A component that accepts a *slog.Logger declares its dependency honestly and
// can be handed a logger that writes to a buffer in tests; one that reaches for
// a package-level global can do neither.
func New(level slog.Level, env string) *slog.Logger {
	opts := &slog.HandlerOptions{
		// Records below this level are discarded cheaply — the arguments to a
		// logger.Debug call are never formatted when the level is info.
		Level: level,

		// AddSource records the file and line of every log call, which costs a
		// runtime.Caller per record. Worth it locally, not in production.
		AddSource: env == "dev",
	}

	// slog.Handler is an interface: a set of required methods. Both concrete
	// handler types below satisfy it without ever declaring that they do —
	// Go interfaces are structural, so there is no "implements" keyword.
	var h slog.Handler
	if env == "prod" {
		// JSON: one object per line, for log aggregators to parse.
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		// Text: key=value, for humans reading a terminal.
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	// Logs go to stdout, not stderr: 12-factor says the app writes to stdout and
	// the platform decides where that goes.
	return slog.New(h)
}
