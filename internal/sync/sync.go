// Package sync implements the background loop that pushes finalized commands
// to the Ghostwriter oplog.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdsync "sync"
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
	// AutoSync controls whether the background loop automatically pushes all
	// finalized commands. When false the loop is idle; commands must be pushed
	// manually via PushOne. Defaults to true when not set explicitly — callers
	// should pass true for the default auto-push behaviour.
	AutoSync bool
	// OnSynced is called after each command is successfully pushed and marked
	// synced. The shortID of the synced command is passed. May be nil.
	OnSynced func(shortID string)
}

// Syncer runs a background loop that pushes unsynced commands to Ghostwriter.
type Syncer struct {
	store   db.Store
	mu      stdsync.RWMutex
	pusher  Pusher
	cfg     Config
	log     *slog.Logger
	resetCh chan struct{} // buffered(1); signals Run to reset the ticker
}

// New creates a Syncer. logger may be nil — all log calls become no-ops.
// If cfg.Interval is zero it defaults to 30 s.
func New(store db.Store, pusher Pusher, cfg Config, logger *slog.Logger) *Syncer {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	return &Syncer{
		store:   store,
		pusher:  pusher,
		cfg:     cfg,
		log:     logger,
		resetCh: make(chan struct{}, 1),
	}
}

// SetInterval updates the sync interval and signals the running loop to reset
// its ticker immediately. Safe to call from any goroutine.
func (s *Syncer) SetInterval(d time.Duration) {
	if d <= 0 {
		d = 30 * time.Second
	}
	s.mu.Lock()
	s.cfg.Interval = d
	s.mu.Unlock()
	select {
	case s.resetCh <- struct{}{}:
	default:
	}
}

// SetAutoSync updates the auto-sync flag at runtime. When enabled the
// background loop resumes pushing all unsynced commands on the next tick.
// Safe to call from any goroutine.
func (s *Syncer) SetAutoSync(enabled bool) {
	s.mu.Lock()
	s.cfg.AutoSync = enabled
	s.mu.Unlock()
}

// SetPusher swaps the active pusher. The next sync batch uses the new pusher.
// Safe to call from any goroutine.
func (s *Syncer) SetPusher(p Pusher) {
	s.mu.Lock()
	s.pusher = p
	s.mu.Unlock()
}

// Interval returns the configured sync interval.
func (s *Syncer) Interval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Interval
}

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

	s.mu.RLock()
	interval := s.cfg.Interval
	s.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.syncBatch(ctx)
		case <-s.resetCh:
			ticker.Stop()
			s.mu.RLock()
			interval = s.cfg.Interval
			s.mu.RUnlock()
			ticker = time.NewTicker(interval)
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
//
// When AutoSync is false the batch is skipped entirely; commands must be
// pushed individually via PushOne.
func (s *Syncer) syncBatch(ctx context.Context) {
	s.mu.RLock()
	pusher := s.pusher
	autoSync := s.cfg.AutoSync
	s.mu.RUnlock()

	if !autoSync {
		return
	}

	cmds, err := s.store.ListUnsynced(ctx)
	if err != nil {
		s.logWarn("sync: ListUnsynced failed", "err", err)
		return
	}

	for _, cmd := range cmds {
		if ctx.Err() != nil {
			return
		}
		if err := pusher.PushCommand(ctx, cmd); err != nil {
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
		} else if s.cfg.OnSynced != nil {
			s.cfg.OnSynced(cmd.ShortID)
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

// PushOne looks up the command identified by shortID and pushes it to
// Ghostwriter immediately, regardless of the AutoSync setting. It is an
// error if the command does not exist or has already been synced.
func (s *Syncer) PushOne(ctx context.Context, shortID string) error {
	cmd, err := s.store.GetCommandByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("sync: command %q not found: %w", shortID, err)
	}
	if cmd.SyncedGW {
		return fmt.Errorf("sync: command %q is already synced to Ghostwriter", shortID)
	}

	s.mu.RLock()
	pusher := s.pusher
	s.mu.RUnlock()

	if err := pusher.PushCommand(ctx, cmd); err != nil {
		return fmt.Errorf("sync: push %q: %w", shortID, err)
	}
	if err := s.store.MarkCommandSynced(ctx, cmd.ID); err != nil {
		s.logWarn("sync: MarkCommandSynced failed", "command_id", cmd.ID, "err", err)
		return fmt.Errorf("sync: mark synced %q: %w", shortID, err)
	}
	if s.cfg.OnSynced != nil {
		s.cfg.OnSynced(cmd.ShortID)
	}
	return nil
}
