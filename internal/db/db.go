package db

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	ErrNotFound = errors.New("db: record not found")
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
	Seq       int64  // sequential position within the pane
	Command   string
	StartedAt int64  // Unix ms
	EndedAt   *int64 // Unix ms; nil until the next precmd fires
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
	CreatePane(ctx context.Context, paneID string, createdAt int64) error

	// Commands
	CreateCommand(ctx context.Context, cmd *Command) error
	GetCommandByShortID(ctx context.Context, shortID string) (*Command, error)
	ListCommandsByPane(ctx context.Context, paneID string) ([]*Command, error)
	ListUnsynced(ctx context.Context) ([]*Command, error)
	FinalizeCommand(ctx context.Context, shortID string, endedAt int64) error
	MarkCommandSynced(ctx context.Context, commandID int64) error
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
