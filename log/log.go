// Package log provides a thin construction and context-propagation layer
// on top of the standard [log/slog] package.
//
// It does not define any custom types for logging — all functions accept and
// return standard *slog.Logger values, keeping the package fully compatible
// with the slog ecosystem.
package log

import (
	"io"
	"log/slog"
	"os"
	"time"
)

// Format controls the output format of the logger.
type Format int

const (
	// FormatJSON outputs structured JSON logs. Recommended for production.
	FormatJSON Format = iota
	// FormatText outputs human-readable text logs. Recommended for local development.
	FormatText
)

// options holds the configuration for building a *slog.Logger.
type options struct {
	level  slog.Level
	format Format
	output io.Writer
}

// defaultOptions returns sensible defaults.
func defaultOptions() options {
	return options{
		level:  slog.LevelInfo,
		format: FormatJSON,
		output: os.Stdout,
	}
}

// Option is a functional option for configuring a logger.
type Option func(*options)

// WithLevel sets the minimum log level. Messages below this level are discarded.
// Defaults to [slog.LevelInfo].
func WithLevel(level slog.Level) Option {
	return func(o *options) {
		o.level = level
	}
}

// WithFormat sets the output format.
// Defaults to [FormatJSON].
func WithFormat(format Format) Option {
	return func(o *options) {
		o.format = format
	}
}

// WithOutput sets the writer to which logs are written.
// Defaults to [os.Stdout].
func WithOutput(w io.Writer) Option {
	return func(o *options) {
		o.output = w
	}
}

// New builds a *slog.Logger with the provided options.
//
// Example:
//
//	logger := log.New(
//	    log.WithLevel(slog.LevelDebug),
//	    log.WithFormat(log.FormatJSON),
//	)
func New(opts ...Option) *slog.Logger {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	handlerOpts := &slog.HandlerOptions{
		Level:       o.level,
		ReplaceAttr: replaceAttr,
	}

	var handler slog.Handler

	switch o.format {
	case FormatText:
		handler = slog.NewTextHandler(o.output, handlerOpts)
	case FormatJSON:
		handler = slog.NewJSONHandler(o.output, handlerOpts)
	}

	return slog.New(handler)
}

// replaceAttr is a [slog.HandlerOptions.ReplaceAttr] function that normalizes
// the time attribute to use RFC3339Nano with UTC timezone, which is more
// portable across log aggregators than the default format.
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		if t, ok := a.Value.Any().(time.Time); ok {
			return slog.String(slog.TimeKey, t.UTC().Format(time.RFC3339Nano))
		}
	}

	return a
}
