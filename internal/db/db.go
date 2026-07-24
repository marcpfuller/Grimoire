package db

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("db: record not found")
	ErrConflict = errors.New("db: short_id already in use")
)

// Pane represents a terminal pane session anchored by GRIMOIRE_PANE_ID.
type Pane struct {
	ID        string
	CreatedAt int64 // Unix ms
}

// Command represents a single executed command within a pane.
type Command struct {
	ID        int64
	ShortID   string // 7-hex operator tag, e.g. "a1b2c3d"
	PaneID    string
	Seq       int64 // sequential position within the pane
	Command   string
	StartedAt int64  // Unix ms
	EndedAt   *int64 // Unix ms; nil until FinalizeCommand is called
	ExitCode  *int   // shell exit status; nil until FinalizeCommand is called
	SyncedGW  bool   // true after a successful Ghostwriter push
}

// OutputChunk is one streamed PTY read captured during a command's execution.
type OutputChunk struct {
	ID        int64
	CommandID int64
	OffsetMs  int64  // milliseconds relative to Command.StartedAt
	Data      []byte // raw PTY bytes
}

// Tag is a retroactive label attached to a command via `grimoire tag`.
type Tag struct {
	ID        int64
	CommandID int64
	Label     string
	CreatedAt int64 // Unix ms
}

// Store is the primary persistence interface. All database interactions go
// through this interface so implementations can be swapped or mocked in tests.
type Store interface {
	// Panes
	// UpsertPane registers paneID, or silently succeeds if it is already
	// registered. This makes re-registration safe when the daemon restarts
	// mid-session and the shell still holds its original GRIMOIRE_PANE_ID.
	UpsertPane(ctx context.Context, paneID string, createdAt int64) error
	CreatePane(ctx context.Context, paneID string, createdAt int64) error

	// Commands
	// ReserveShortID atomically claims shortID for paneID in the
	// pending_commands table. Returns ErrConflict if shortID is already in use
	// in either pending_commands or commands. The same pane may call this again
	// to replace its current reservation.
	ReserveShortID(ctx context.Context, paneID string, shortID string) error
	// CreateCommand claims the pending reservation for cmd.PaneID, assigns the
	// next sequence number, and inserts the row. cmd.ID, cmd.Seq, and
	// cmd.ShortID are populated on success. Any ShortID value on the input
	// struct is ignored — it is always sourced from the reservation.
	// NOTE: the API handler for POST /api/execute must return cmd.ID in the
	// response so the PTY capture layer can reference it in AppendOutputChunk.
	CreateCommand(ctx context.Context, cmd *Command) error
	GetCommandByShortID(ctx context.Context, shortID string) (*Command, error)
	ListCommandsByPane(ctx context.Context, paneID string) ([]*Command, error)
	ListUnsynced(ctx context.Context) ([]*Command, error)
	FinalizeCommand(ctx context.Context, shortID string, endedAt int64, exitCode int) error
	MarkCommandSynced(ctx context.Context, commandID int64) error
	// ShortIDExists is a diagnostic utility that reports whether shortID is
	// already in use in commands or pending_commands. It is NOT in the critical
	// path — the API handler for GET /api/next-id should retry on ErrConflict
	// from ReserveShortID rather than pre-checking with this method.
	ShortIDExists(ctx context.Context, shortID string) (bool, error)

	// Output
	AppendOutputChunk(ctx context.Context, chunk *OutputChunk) error
	GetOutputChunks(ctx context.Context, commandID int64) ([]*OutputChunk, error)

	// Tags
	AddTag(ctx context.Context, commandID int64, label string, createdAt int64) error
	GetTagsByCommand(ctx context.Context, commandID int64) ([]*Tag, error)

	// Lifecycle
	Close() error
}
