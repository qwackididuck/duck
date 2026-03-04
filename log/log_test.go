package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/qwackididuck/duck/log"
)

// logEntry is a helper for decoding JSON log lines in tests.
type logEntry struct {
	Level   string
	Message string
	Time    string
	Extra   map[string]json.RawMessage
}

func (e *logEntry) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal log entry: %w", err)
	}

	if v, ok := raw["level"]; ok {
		_ = json.Unmarshal(v, &e.Level)
	}

	if v, ok := raw["time"]; ok {
		_ = json.Unmarshal(v, &e.Time)
	}

	if v, ok := raw["msg"]; ok {
		_ = json.Unmarshal(v, &e.Message)
	}

	e.Extra = raw

	return nil
}

func decodeLastLine(t *testing.T, buf *bytes.Buffer) logEntry {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")

	var entry logEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("decodeLastLine: %v\nraw: %s", err, lines[len(lines)-1])
	}

	return entry
}

func checkField(t *testing.T, entry logEntry, key, wantJSON string) {
	t.Helper()

	v, ok := entry.Extra[key]
	if !ok {
		t.Errorf("expected field %q to be present in log output", key)

		return
	}

	if string(v) != wantJSON {
		t.Errorf("field %q: expected %s, got %s", key, wantJSON, string(v))
	}
}

// --- New() ---

func TestNew_format(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		format         log.Format
		message        string
		wantJSONParsed bool
		wantContains   string
	}{
		{
			name:           "JSON format produces parseable output",
			format:         log.FormatJSON,
			message:        "hello json",
			wantJSONParsed: true,
		},
		{
			name:         "Text format contains the message",
			format:       log.FormatText,
			message:      "hello text",
			wantContains: "hello text",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := log.New(log.WithFormat(tc.format), log.WithOutput(&buf))

			logger.Info(tc.message)

			if tc.wantJSONParsed {
				entry := decodeLastLine(t, &buf)
				if entry.Message != tc.message {
					t.Errorf("expected msg %q, got %q", tc.message, entry.Message)
				}
			}

			if tc.wantContains != "" && !strings.Contains(buf.String(), tc.wantContains) {
				t.Errorf("expected output to contain %q, got: %s", tc.wantContains, buf.String())
			}
		})
	}
}

func TestNew_levelFiltering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		level        slog.Level
		wantFiltered []string
		wantPresent  []string
	}{
		{
			name:         "Warn level filters debug and info",
			level:        slog.LevelWarn,
			wantFiltered: []string{"debug-msg", "info-msg"},
			wantPresent:  []string{"warn-msg"},
		},
		{
			name:        "Debug level keeps all messages",
			level:       slog.LevelDebug,
			wantPresent: []string{"debug-msg", "info-msg", "warn-msg"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := log.New(log.WithLevel(tc.level), log.WithOutput(&buf))

			logger.Debug("debug-msg")
			logger.Info("info-msg")
			logger.Warn("warn-msg")

			output := buf.String()

			for _, msg := range tc.wantFiltered {
				if strings.Contains(output, msg) {
					t.Errorf("message %q should have been filtered at level %s", msg, tc.level)
				}
			}

			for _, msg := range tc.wantPresent {
				if !strings.Contains(output, msg) {
					t.Errorf("message %q should be present at level %s", msg, tc.level)
				}
			}
		})
	}
}

func TestNew_timeIsRFC3339Nano(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := log.New(log.WithOutput(&buf))

	logger.Info("timestamp test")

	entry := decodeLastLine(t, &buf)

	if !strings.Contains(entry.Time, "T") {
		t.Errorf("expected RFC3339Nano time format, got: %q", entry.Time)
	}
}

// --- ContextWithAttrs / FromContext ---

func TestContextWithAttrs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupCtx   func(ctx context.Context) context.Context
		wantFields map[string]string
		wantAbsent []string
	}{
		{
			name: "single attr propagates",
			setupCtx: func(ctx context.Context) context.Context {
				return log.ContextWithAttrs(ctx, slog.String("request_id", "abc-123"))
			},
			wantFields: map[string]string{"request_id": `"abc-123"`},
		},
		{
			name: "multiple attrs propagate",
			setupCtx: func(ctx context.Context) context.Context {
				return log.ContextWithAttrs(ctx,
					slog.String("request_id", "abc-123"),
					slog.String("component", "api"),
				)
			},
			wantFields: map[string]string{
				"request_id": `"abc-123"`,
				"component":  `"api"`,
			},
		},
		{
			name: "successive calls accumulate",
			setupCtx: func(ctx context.Context) context.Context {
				ctx = log.ContextWithAttrs(ctx, slog.String("request_id", "abc-123"))

				return log.ContextWithAttrs(ctx, slog.String("user_id", "usr-456"))
			},
			wantFields: map[string]string{
				"request_id": `"abc-123"`,
				"user_id":    `"usr-456"`,
			},
		},
		{
			name: "empty context produces no extra fields",
			setupCtx: func(ctx context.Context) context.Context {
				return ctx
			},
			wantAbsent: []string{"request_id", "user_id", "component"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := log.New(log.WithOutput(&buf))
			ctx := tc.setupCtx(context.Background())

			log.FromContext(ctx, logger).Info("test message")

			entry := decodeLastLine(t, &buf)

			for key, wantVal := range tc.wantFields {
				checkField(t, entry, key, wantVal)
			}

			for _, key := range tc.wantAbsent {
				if _, ok := entry.Extra[key]; ok {
					t.Errorf("field %q should not be present", key)
				}
			}
		})
	}
}

func TestContextWithAttrs_doesNotMutateParent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := log.New(log.WithOutput(&buf))

	parent := log.ContextWithAttrs(context.Background(), slog.String("request_id", "abc-123"))
	_ = log.ContextWithAttrs(parent, slog.String("user_id", "usr-456"))

	// Log from parent — must NOT contain user_id added only to child.
	log.FromContext(parent, logger).Info("parent log")

	entry := decodeLastLine(t, &buf)

	if _, ok := entry.Extra["user_id"]; ok {
		t.Error("parent context should not contain user_id added only to child")
	}
}
