// Package logger builds the two-sink slog setup used by Grimoire:
//
//   - JSON handler → size-rotating file at <Dir>/<Filename>  (machine-readable, full detail)
//   - Text handler → os.Stderr                               (WARN+ only, operator-visible)
//
// Log files are named grimoire-<project_id>.log and are created per engagement.
// If the file already exists it is appended to, not truncated.
// A single *slog.Logger fans records to both sinks via multiHandler so no
// message is ever written to only one destination.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config holds logger configuration sourced from config.yaml.
type Config struct {
	// Dir is the directory where log files are written. Must not be empty.
	Dir string
	// Filename is the log file name within Dir, e.g. "grimoire-42.log".
	// The caller is responsible for constructing the project-scoped name.
	// Must not be empty.
	Filename string
	// Level is the minimum level for the JSON file handler (default INFO).
	Level slog.Level
	// MaxMB is the maximum log file size in megabytes before rotation (default 50).
	MaxMB int
	// MaxBackups is the number of rotated files to keep (default 5).
	MaxBackups int
}

// New builds a *slog.Logger that fans records to a rotating JSON file and to
// os.Stderr (WARN+ only in text format). The returned *lumberjack.Logger must
// be closed on daemon shutdown to flush pending writes.
// The returned *slog.LevelVar controls the JSON file handler's minimum level
// at runtime (e.g. lvl.Set(slog.LevelDebug) for a grimoire log level command).
// If the log file already exists it is appended to, not truncated.
func New(cfg Config) (*slog.Logger, *lumberjack.Logger, *slog.LevelVar, error) {
	if cfg.Dir == "" {
		return nil, nil, nil, fmt.Errorf("logger: Dir must not be empty")
	}
	if cfg.Filename == "" {
		return nil, nil, nil, fmt.Errorf("logger: Filename must not be empty")
	}
	if cfg.Filename != filepath.Base(cfg.Filename) {
		return nil, nil, nil, fmt.Errorf("logger: Filename must be a plain file name, got %q", cfg.Filename)
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("logger: create log dir %q: %w", cfg.Dir, err)
	}

	// Probe that the log file is writable before constructing handlers.
	// Lumberjack opens lazily; without this, an unwritable path fails silently
	// on the first log call rather than at startup.
	logPath := filepath.Join(cfg.Dir, cfg.Filename)
	probe, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // logPath = MkdirAll-validated Dir + filepath.Base-validated Filename
	if err != nil {
		return nil, nil, nil, fmt.Errorf("logger: log file not writable %q: %w", logPath, err)
	}
	_ = probe.Close()

	maxMB := cfg.MaxMB
	if maxMB <= 0 {
		maxMB = 50
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 5
	}

	rotator := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxMB,
		MaxBackups: maxBackups,
		Compress:   true,
	}

	lvl := new(slog.LevelVar)
	lvl.Set(cfg.Level)

	// JSON file: captures everything at or above cfg.Level (default INFO).
	jsonHandler := slog.NewJSONHandler(rotator, &slog.HandlerOptions{Level: lvl})

	// Text stderr: WARN and above only — keeps the operator's terminal clean
	// during normal operation, surfaces errors without interrupting the PTY.
	textHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})

	return slog.New(&multiHandler{handlers: []slog.Handler{jsonHandler, textHandler}}), rotator, lvl, nil
}

// multiHandler fans a single slog.Record to every registered handler.
// WithAttrs and WithGroup return a new multiHandler whose child handlers each
// carry the decoration, so pre-bound attributes (e.g. pane_id) propagate
// correctly to both sinks.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle delivers r to every handler that accepts its level. All handlers are
// called regardless of individual failures (best-effort fan-out). The last
// non-nil error is returned; if no handler fails, nil is returned.
func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var last error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				last = err
			}
		}
	}
	return last
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: hs}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: hs}
}
