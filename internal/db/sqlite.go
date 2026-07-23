package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS panes (
    id         TEXT    PRIMARY KEY,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
    id         INTEGER PRIMARY KEY,
    short_id   TEXT    NOT NULL UNIQUE,
    pane_id    TEXT    NOT NULL REFERENCES panes(id),
    seq        INTEGER NOT NULL,
    command    TEXT    NOT NULL,
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
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
    created_at INTEGER NOT NULL
);

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

	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// --- Panes ---

func (s *sqliteStore) CreatePane(ctx context.Context, paneID string, createdAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO panes (id, created_at) VALUES (?, ?)`,
		paneID, createdAt,
	)
	return err
}

// --- Commands ---

// CreateCommand inserts a new command record. The sequence number is derived
// atomically inside the transaction so concurrent panes never collide.
// cmd.ID and cmd.Seq are populated on success.
func (s *sqliteStore) CreateCommand(ctx context.Context, cmd *Command) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seq int64
	if err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM commands WHERE pane_id = ?`,
		cmd.PaneID,
	).Scan(&seq); err != nil {
		return fmt.Errorf("db: next seq: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO commands (short_id, pane_id, seq, command, started_at)
		 VALUES (?, ?, ?, ?, ?)`,
		cmd.ShortID, cmd.PaneID, seq, cmd.Command, cmd.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("db: insert command: %w", err)
	}

	cmd.ID, _ = res.LastInsertId()
	cmd.Seq = seq

	return tx.Commit()
}

func (s *sqliteStore) GetCommandByShortID(ctx context.Context, shortID string) (*Command, error) {
	cmd := &Command{}
	var syncedGW int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, short_id, pane_id, seq, command, started_at, ended_at, synced_gw
		 FROM commands WHERE short_id = ?`,
		shortID,
	).Scan(&cmd.ID, &cmd.ShortID, &cmd.PaneID, &cmd.Seq, &cmd.Command,
		&cmd.StartedAt, &cmd.EndedAt, &syncedGW)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cmd.SyncedGW = syncedGW != 0
	return cmd, nil
}

func (s *sqliteStore) ListCommandsByPane(ctx context.Context, paneID string) ([]*Command, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, short_id, pane_id, seq, command, started_at, ended_at, synced_gw
		 FROM commands WHERE pane_id = ? ORDER BY seq`,
		paneID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

func (s *sqliteStore) ListUnsynced(ctx context.Context) ([]*Command, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, short_id, pane_id, seq, command, started_at, ended_at, synced_gw
		 FROM commands WHERE synced_gw = 0 AND ended_at IS NOT NULL ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

func (s *sqliteStore) FinalizeCommand(ctx context.Context, shortID string, endedAt int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE commands SET ended_at = ? WHERE short_id = ?`,
		endedAt, shortID,
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

func (s *sqliteStore) ShortIDExists(ctx context.Context, shortID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM commands WHERE short_id = ?`, shortID,
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

func (s *sqliteStore) GetOutputChunks(ctx context.Context, commandID int64) ([]*OutputChunk, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, command_id, offset_ms, data
		 FROM output_chunks WHERE command_id = ? ORDER BY offset_ms`,
		commandID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*OutputChunk
	for rows.Next() {
		c := &OutputChunk{}
		if err := rows.Scan(&c.ID, &c.CommandID, &c.OffsetMs, &c.Data); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// --- Tags ---

func (s *sqliteStore) AddTag(ctx context.Context, commandID int64, label string, createdAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tags (command_id, label, created_at) VALUES (?, ?, ?)`,
		commandID, label, createdAt,
	)
	return err
}

func (s *sqliteStore) GetTagsByCommand(ctx context.Context, commandID int64) ([]*Tag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, command_id, label, created_at FROM tags WHERE command_id = ? ORDER BY created_at`,
		commandID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*Tag
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.CommandID, &t.Label, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// --- Helpers ---

func scanCommands(rows *sql.Rows) ([]*Command, error) {
	var cmds []*Command
	for rows.Next() {
		cmd := &Command{}
		var syncedGW int64
		if err := rows.Scan(&cmd.ID, &cmd.ShortID, &cmd.PaneID, &cmd.Seq,
			&cmd.Command, &cmd.StartedAt, &cmd.EndedAt, &syncedGW); err != nil {
			return nil, err
		}
		cmd.SyncedGW = syncedGW != 0
		cmds = append(cmds, cmd)
	}
	return cmds, rows.Err()
}
