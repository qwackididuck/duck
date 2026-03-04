package log

import (
	"context"
	"log/slog"
)

// ctxKey is the unexported key type used to store log attributes in a context.
// Using a private type prevents collisions with other packages.
type ctxKey struct{}

// ContextWithAttrs returns a copy of ctx enriched with the provided slog
// attributes. Successive calls accumulate attributes — they do not overwrite
// previous ones.
//
// This function is typically called once per request in a middleware, after
// extracting identifiers such as a request ID or a trace ID:
//
//	ctx = log.ContextWithAttrs(ctx,
//	    slog.String("request_id", requestID),
//	    slog.String("component", "api"),
//	)
func ContextWithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	existing := attrsFromContext(ctx)

	merged := make([]slog.Attr, len(existing), len(existing)+len(attrs))
	copy(merged, existing)
	merged = append(merged, attrs...)

	return context.WithValue(ctx, ctxKey{}, merged)
}

// FromContext returns a *slog.Logger enriched with all attributes previously
// stored in ctx via [ContextWithAttrs]. If the context carries no attributes,
// the base logger is returned unchanged.
//
// The base logger is typically the application-level logger built with [New]
// and stored in a service or handler struct:
//
//	log.FromContext(ctx, s.logger).Info("processing request",
//	    slog.String("user_id", userID),
//	)
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	attrs := attrsFromContext(ctx)
	if len(attrs) == 0 {
		return base
	}

	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}

	return base.With(args...)
}

// attrsFromContext returns the slog attributes stored in ctx, or nil if none.
func attrsFromContext(ctx context.Context) []slog.Attr {
	v, _ := ctx.Value(ctxKey{}).([]slog.Attr)

	return v
}
