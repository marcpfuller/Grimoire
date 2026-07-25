package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS panes (
    id         TEXT    PRIMARY KEY,
    project_id TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
    id         INTEGER PRIMARY KEY,
    short_id   TEXT    NOT NULL UNIQUE,
    pane_id    TEXT    NOT NULL REFERENCES panes(id),
    project_id TEXT    NOT NULL DEFAULT '',
    seq        INTEGER NOT NULL,
    command    TEXT    NOT NULL,
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
    exit_code  INTEGER,
    synced_gw  INTEGER NOT NULL DEFAULT 0,
    UNIQUE(pane_id, seq)
);

CREATE TABLE IF NOT EXISTS output_chunks (
    id         INTEGER PRIMARY KEY,
    command_id INTEGER NOT NULL REFERENCES commands(id),
    offset_ms  INTEGER NOT NULL,
    data       BLOB    NOT NULL
);

CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY,
    command_id INTEGER NOT NULL REFERENCES commands(id),
    label      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    -- Deduplication enforced at both schema (new DBs) and application layer
    -- (AddTag uses INSERT OR IGNORE) so existing DBs without this constraint
    -- are also protected.
    UNIQUE(command_id, label)
);

CREATE TABLE IF NOT EXISTS pending_commands (
    pane_id  TEXT PRIMARY KEY REFERENCES panes(id),
    short_id TEXT NOT NULL UNIQUE
);
-- One row per active pane. If the shell exits abnormally (e.g. SIGKILL) before
-- preexec fires, this row is never consumed by CreateCommand and remains until
-- the next ReserveShortID call for the same pane UPSERT-overwrites it. This is
-- intentional: the leak is bounded to one row per pane and self-heals on the
-- next prompt draw.

CREATE INDEX IF NOT EXISTS idx_commands_pane_seq     ON commands(pane_id, seq);
CREATE INDEX IF NOT EXISTS idx_chunks_command_offset ON output_chunks(command_id, offset_ms);
`

type sqliteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and returns a Store.
// WAL mode, NORMAL synchronous, and foreign key enforcement are set before
// any schema work. A single connection is used so the pragmas remain in effect
// for the lifetime of the process.
func Open(path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}

	// Single connection ensures per-connection pragmas are always active.
	db.SetMaxOpenConns(1)

	if _, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA foreign_keys=ON;
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: set pragmas: %w", err)
	}

	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: apply schema: %w", err)
	}

	if err = applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: migrate: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// --- Migrations ---

// migration is a single forward-only schema change identified by a version
// number stored in PRAGMA user_version.
type migration struct {
	version int
	sql     string
}

// migrations is the ordered list of incremental schema changes applied after
// the base schema. Append new entries at the end — never edit or reorder
// existing ones.
//
// VERSION HISTORY:
//
//	1: enforce UNIQUE(command_id, label) on tags for databases created before
//	   the constraint was added to the base schema DDL.
var migrations = []migration{
	{
		version: 1,
		sql:     `CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_command_label ON tags(command_id, label)`,
	},
}

// applyMigrations reads PRAGMA user_version and runs any pending migrations in
// order, advancing user_version after each one. The version integer is stored
// in the SQLite file header — no schema table is required.
//
// PRAGMA user_version = N does not support the ? placeholder; the integer is
// formatted directly into the statement. This is safe because migration.version
// is a hardcoded int, not user-supplied input.
func applyMigrations(db *sql.DB) error {
	var ver int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&ver); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for _, m := range migrations {
		if ver >= m.version {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		// G201: m.version is a hardcoded int — no injection risk.
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil { //nolint:gosec
			return fmt.Errorf("migration %d: advance version: %w", m.version, err)
		}
		ver = m.version
	}
	return nil
}

// --- Panes ---

// UpsertPane registers paneID with its Ghostwriter projectID, or silently
// succeeds if already registered. Safe to call on daemon restart when the
// shell still holds its original GRIMOIRE_PANE_ID.
//
// INSERT OR IGNORE: if the pane is already registered (e.g. daemon restart),
// the existing row — including its projectID — is preserved unchanged.
// Project IDs are not expected to change mid-session.
func (s *sqliteStore) UpsertPane(ctx context.Context, paneID string, projectID string, createdAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO panes (id, project_id, created_at) VALUES (?, ?, ?)`,
		paneID, projectID, createdAt,
	)
	return err
}

func (s *sqliteStore) CreatePane(ctx context.Context, paneID string, projectID string, createdAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO panes (id, project_id, created_at) VALUES (?, ?, ?)`,
		paneID, projectID, createdAt,
	)
	return err
}

// DeregisterPane removes the pending short ID reservation for paneID so the
// reserved ID is immediately available to other panes. The pane row and all
// its commands are retained for historical purposes.
//
// DELETE is a no-op if paneID has no pending reservation or does not exist;
// both cases are valid — the shell may call this on abnormal exit before ever
// reserving a short ID, and the silence is intentional.
func (s *sqliteStore) DeregisterPane(ctx context.Context, paneID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_commands WHERE pane_id = ?`,
		paneID,
	)
	return err
}

// --- Commands ---

// ReserveShortID atomically claims shortID for paneID in pending_commands.
// The same pane may call this repeatedly to refresh its reservation; the
// UPSERT overwrites any previously restored reservation (see CreateCommand
// rollback note below), which is intentional — the old ID is abandoned
// cleanly because it was never inserted into commands.
// Returns ErrConflict if shortID is already in use in commands or pending_commands.
func (s *sqliteStore) ReserveShortID(ctx context.Context, paneID string, shortID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Reject if already permanently claimed.
	var inCommands int
	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM commands WHERE short_id = ?`, shortID,
	).Scan(&inCommands); err != nil {
		return fmt.Errorf("db: check commands: %w", err)
	}
	if inCommands > 0 {
		return ErrConflict
	}

	// Reject if already reserved by a different pane.
	var inPending int
	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM pending_commands WHERE short_id = ? AND pane_id != ?`,
		shortID, paneID,
	).Scan(&inPending); err != nil {
		return fmt.Errorf("db: check pending: %w", err)
	}
	if inPending > 0 {
		return ErrConflict
	}

	// UPSERT: same pane refreshes its reservation; new pane gets a fresh one.
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO pending_commands (pane_id, short_id) VALUES (?, ?)
		 ON CONFLICT(pane_id) DO UPDATE SET short_id = excluded.short_id`,
		paneID, shortID,
	); err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return ErrNotFound
		}
		return fmt.Errorf("db: reserve: %w", err)
	}
	return tx.Commit()
}

// CreateCommand claims the pending reservation for cmd.PaneID, assigns the
// next sequence number, and inserts the row. cmd.ID, cmd.Seq, and cmd.ShortID
// are populated on success. Any ShortID value on the input struct is ignored.
//
// Rollback semantics: the DELETE of pending_commands is inside this
// transaction. If the transaction rolls back for any reason (context
// cancellation, constraint violation, etc.), the reservation is restored
// atomically — the short_id shown in the prompt is still the active
// reservation and a retry will succeed. If the shell hook instead calls
// GET /api/next-id after a failure, ReserveShortID's UPSERT overwrites the
// restored reservation with the new ID; the old short_id is silently
// abandoned. This is safe because it was never committed to commands.
func (s *sqliteStore) CreateCommand(ctx context.Context, cmd *Command) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Claim the reservation atomically.
	var shortID string
	if err = tx.QueryRowContext(ctx,
		`SELECT short_id FROM pending_commands WHERE pane_id = ?`, cmd.PaneID,
	).Scan(&shortID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("db: no pending reservation for pane %s: %w", cmd.PaneID, ErrNotFound)
		}
		return fmt.Errorf("db: claim reservation: %w", err)
	}

	if _, err = tx.ExecContext(ctx,
		`DELETE FROM pending_commands WHERE pane_id = ?`, cmd.PaneID,
	); err != nil {
		return fmt.Errorf("db: delete reservation: %w", err)
	}

	// Propagate project_id from the owning pane to the command row so
	// the sync layer can group commands by project without a JOIN.
	var projectID string
	if err = tx.QueryRowContext(ctx,
		`SELECT project_id FROM panes WHERE id = ?`, cmd.PaneID,
	).Scan(&projectID); err != nil {
		return fmt.Errorf("db: get pane project_id: %w", err)
	}

	var seq int64
	if err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM commands WHERE pane_id = ?`, cmd.PaneID,
	).Scan(&seq); err != nil {
		return fmt.Errorf("db: next seq: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO commands (short_id, pane_id, project_id, seq, command, started_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		shortID, cmd.PaneID, projectID, seq, cmd.Command, cmd.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("db: insert command: %w", err)
	}

	cmd.ID, _ = res.LastInsertId()
	cmd.Seq = seq
	cmd.ShortID = shortID
	cmd.ProjectID = projectID

	return tx.Commit()
}

func (s *sqliteStore) GetCommandByShortID(ctx context.Context, shortID string) (*Command, error) {
	cmd := &Command{}
	var syncedGW int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, short_id, pane_id, project_id, seq, command, started_at, ended_at, exit_code, synced_gw
		 FROM commands WHERE short_id = ?`,
		shortID,
	).Scan(&cmd.ID, &cmd.ShortID, &cmd.PaneID, &cmd.ProjectID, &cmd.Seq, &cmd.Command,
		&cmd.StartedAt, &cmd.EndedAt, &cmd.ExitCode, &syncedGW)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cmd.SyncedGW = syncedGW != 0
	return cmd, nil
}

func (s *sqliteStore) ListCommandsByPane(ctx context.Context, paneID string) (cmds []*Command, err error) {
	rows, qErr := s.db.QueryContext(ctx,
		`SELECT id, short_id, pane_id, project_id, seq, command, started_at, ended_at, exit_code, synced_gw
		 FROM commands WHERE pane_id = ? ORDER BY seq`,
		paneID,
	)
	if qErr != nil {
		return nil, qErr
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	cmds, err = scanCommands(rows)
	return
}

func (s *sqliteStore) ListUnsynced(ctx context.Context) (cmds []*Command, err error) {
	rows, qErr := s.db.QueryContext(ctx,
		`SELECT id, short_id, pane_id, project_id, seq, command, started_at, ended_at, exit_code, synced_gw
		 FROM commands WHERE synced_gw = 0 AND ended_at IS NOT NULL ORDER BY id`,
	)
	if qErr != nil {
		return nil, qErr
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	cmds, err = scanCommands(rows)
	return
}

func (s *sqliteStore) FinalizeCommand(ctx context.Context, shortID string, endedAt int64, exitCode int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE commands SET ended_at = ?, exit_code = ? WHERE short_id = ?`,
		endedAt, exitCode, shortID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) MarkCommandSynced(ctx context.Context, commandID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE commands SET synced_gw = 1 WHERE id = ?`,
		commandID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ShortIDExists reports whether shortID is already in use in either the
// commands table (permanently claimed) or pending_commands (reserved).
func (s *sqliteStore) ShortIDExists(ctx context.Context, shortID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM (
			SELECT 1 FROM commands        WHERE short_id = ?
			UNION ALL
			SELECT 1 FROM pending_commands WHERE short_id = ?
		)`, shortID, shortID,
	).Scan(&count)
	return count > 0, err
}

// --- Output ---

func (s *sqliteStore) AppendOutputChunk(ctx context.Context, chunk *OutputChunk) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO output_chunks (command_id, offset_ms, data) VALUES (?, ?, ?)`,
		chunk.CommandID, chunk.OffsetMs, chunk.Data,
	)
	if err != nil {
		return err
	}
	chunk.ID, _ = res.LastInsertId()
	return nil
}

func (s *sqliteStore) GetOutputChunks(ctx context.Context, commandID int64) (chunks []*OutputChunk, err error) {
	rows, qErr := s.db.QueryContext(ctx,
		`SELECT id, command_id, offset_ms, data
		 FROM output_chunks WHERE command_id = ? ORDER BY offset_ms`,
		commandID,
	)
	if qErr != nil {
		return nil, qErr
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for rows.Next() {
		c := &OutputChunk{}
		if err = rows.Scan(&c.ID, &c.CommandID, &c.OffsetMs, &c.Data); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	err = rows.Err()
	return
}

// --- Tags ---

func (s *sqliteStore) AddTag(ctx context.Context, commandID int64, label string, createdAt int64) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tags (command_id, label, created_at) VALUES (?, ?, ?)`,
		commandID, label, createdAt,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDuplicateTag
	}
	return nil
}

func (s *sqliteStore) GetTagsByCommand(ctx context.Context, commandID int64) (tags []*Tag, err error) {
	rows, qErr := s.db.QueryContext(ctx,
		`SELECT id, command_id, label, created_at FROM tags WHERE command_id = ? ORDER BY created_at`,
		commandID,
	)
	if qErr != nil {
		return nil, qErr
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for rows.Next() {
		t := &Tag{}
		if err = rows.Scan(&t.ID, &t.CommandID, &t.Label, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	err = rows.Err()
	return
}

// --- Helpers ---

func scanCommands(rows *sql.Rows) ([]*Command, error) {
	var cmds []*Command
	for rows.Next() {
		cmd := &Command{}
		var syncedGW int64
		if err := rows.Scan(&cmd.ID, &cmd.ShortID, &cmd.PaneID, &cmd.ProjectID, &cmd.Seq,
			&cmd.Command, &cmd.StartedAt, &cmd.EndedAt, &cmd.ExitCode, &syncedGW); err != nil {
			return nil, err
		}
		cmd.SyncedGW = syncedGW != 0
		cmds = append(cmds, cmd)
	}
	return cmds, rows.Err()
}
