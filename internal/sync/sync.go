// Package sync implements the background loop that pushes finalized commands
// to the Ghostwriter oplog.
package sync

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/marcpfuller/grimoire/internal/db"
)

// AuthError indicates an authentication or configuration problem that requires
// operator intervention (e.g. invalid or expired service token, wrong URL).
// When Pusher.PushCommand returns an *AuthError the sync loop logs at ERROR
// level — automatic retry will not resolve it.
type AuthError struct {
	Err error
}

func (e *AuthError) Error() string { return "auth: " + e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// Pusher pushes a single finalized command to the Ghostwriter oplog.
//
// Implementations must return *AuthError for 401/403 responses so the sync
// loop can distinguish operator-action errors from transient network failures.
//
// TODO: Add CheckProjects(ctx context.Context, projectIDs []string) (active []string, err error)
// once the log-cleaning feature is implemented — it will validate which project
// IDs still have active Ghostwriter oplogs before pruning local log files.
type Pusher interface {
	PushCommand(ctx context.Context, cmd *db.Command) error
}

// Config holds sync loop tunables.
type Config struct {
	// Interval between sync ticks. Defaults to 30 s when zero.
	Interval time.Duration
}

// Syncer runs a background loop that pushes unsynced commands to Ghostwriter.
type Syncer struct {
	store  db.Store
	pusher Pusher
	cfg    Config
	log    *slog.Logger
}

// New creates a Syncer. logger may be nil — all log calls become no-ops.
// If cfg.Interval is zero it defaults to 30 s.
func New(store db.Store, pusher Pusher, cfg Config, logger *slog.Logger) *Syncer {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	return &Syncer{store: store, pusher: pusher, cfg: cfg, log: logger}
}

// Interval returns the configured sync interval.
func (s *Syncer) Interval() time.Duration { return s.cfg.Interval }

// RunOnce executes a single sync batch synchronously. Primarily used in tests;
// in production code prefer Run.
func (s *Syncer) RunOnce(ctx context.Context) {
	s.syncBatch(ctx)
}

// Run blocks until ctx is cancelled. It fires one sync batch immediately, then
// repeats every cfg.Interval. On shutdown it drains the current in-flight batch
// with a hard 10 s timeout before returning.
func (s *Syncer) Run(ctx context.Context) {
	s.syncBatch(ctx)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.syncBatch(ctx)
		case <-ctx.Done():
			drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.syncBatch(drainCtx)
			cancel()
			return
		}
	}
}

// syncBatch fetches all unsynced commands and pushes them one at a time.
// Each command is best-effort: a failure is logged and the loop continues.
// SyncedGW remains false on failure so the command is retried on the next tick.
//
// Transient failures (network, 5xx) are logged at WARN.
// Auth/config failures (*AuthError: 401, 403) are logged at ERROR — the
// operator must act.
func (s *Syncer) syncBatch(ctx context.Context) {
	cmds, err := s.store.ListUnsynced(ctx)
	if err != nil {
		s.logWarn("sync: ListUnsynced failed", "err", err)
		return
	}

	for _, cmd := range cmds {
		if ctx.Err() != nil {
			return
		}
		if err := s.pusher.PushCommand(ctx, cmd); err != nil {
			var authErr *AuthError
			if errors.As(err, &authErr) {
				s.logError("sync: push failed — fix token/config",
					"command_id", cmd.ID, "project_id", cmd.ProjectID, "err", err)
			} else {
				s.logWarn("sync: push failed, will retry",
					"command_id", cmd.ID, "err", err)
			}
			continue
		}
		if err := s.store.MarkCommandSynced(ctx, cmd.ID); err != nil {
			s.logWarn("sync: MarkCommandSynced failed", "command_id", cmd.ID, "err", err)
		}
	}
}

func (s *Syncer) logWarn(msg string, args ...any) {
	if s.log != nil {
		s.log.Warn(msg, args...)
	}
}

func (s *Syncer) logError(msg string, args ...any) {
	if s.log != nil {
		s.log.Error(msg, args...)
	}
}
