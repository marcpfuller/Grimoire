package db

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	ErrNotFound     = errors.New("db: record not found")
	ErrConflict     = errors.New("db: short_id already in use")
	ErrDuplicateTag = errors.New("db: tag label already applied to this command")
	ErrNameTooLong  = errors.New("db: pane name exceeds maximum length")
)

// MaxPaneNameLength is the maximum number of characters allowed in a
// user-supplied pane name (see UpsertPane). Enforced here so both the CLI
// (fail-fast before a shell is spawned) and the store (defense in depth for
// any other caller) agree on the same limit.
const MaxPaneNameLength = 20

// Pane represents a terminal pane session anchored by GRIMOIRE_PANE_ID.
type Pane struct {
	ID        string
	Name      string // readable display name, e.g. "quiet-star-0718"; generated once at creation
	OplogID   int    // Ghostwriter oplog ID this pane writes to (0 = unset)
	OplogName string // oplog display name, snapshot at registration time
	CreatedAt int64  // Unix ms
	Width     int    // terminal columns at session start
	Height    int    // terminal rows at session start
}

// Command represents a single executed command within a pane.
type Command struct {
	ID        int64
	ShortID   string // 7-hex operator tag, e.g. "a1b2c3d"
	PaneID    string
	OplogID   int    // pane oplog, or the recorded remote target after a successful override push
	OplogName string // oplog display name, derived via JOIN on panes at read time
	Seq       int64  // sequential position within the pane
	Command   string
	Prompt    string // shell prompt text sent by the preexec hook (e.g. "user@host:~/dir$ ")
	StartedAt int64  // Unix ms
	EndedAt   *int64 // Unix ms; nil until FinalizeCommand is called
	ExitCode  *int   // shell exit status; nil until FinalizeCommand is called
	SyncedGW  bool   // true after this command has been created in Ghostwriter
	// SyncRevision increments whenever metadata changes. SyncPending remains
	// true until that exact revision (or a newer one) is pushed successfully.
	SyncRevision int64
	SyncPending  bool
}

// DeleteCommandResult describes the owning pane state after a command and its
// associated local records have been deleted.
type DeleteCommandResult struct {
	PaneID      string
	PaneDeleted bool
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

// TagWithCommand is returned by ListAllTagsWithCommands and enriches Tag with
// denormalised fields from the parent command row, avoiding N+1 lookups.
type TagWithCommand struct {
	ID          int64
	CommandID   int64
	ShortID     string
	Label       string
	CommandText string
	PaneID      string
	CreatedAt   int64 // Unix ms
}

// CommandMetadata stores operator-supplied annotations for a command. When no
// row exists, DefaultCommandMetadata supplies the tool and outcome description
// used by both the API and Ghostwriter sync.
type CommandMetadata struct {
	CommandID     int64
	Operator      string
	Tool          string
	UserContext   string
	SourceIP      string
	DestinationIP string
	Description   string
	Comments      string
	UpdatedAt     int64 // Unix ms
}

// Store is the primary persistence interface. All database interactions go
// through this interface so implementations can be swapped or mocked in tests.
type Store interface {
	// Panes
	// UpsertPane registers paneID with its Ghostwriter oplog ID and name, or
	// silently succeeds if already registered. This makes re-registration safe
	// when the daemon restarts mid-session and the shell still holds its
	// original GRIMOIRE_PANE_ID.
	//
	// name is an optional user-supplied display name (e.g. via `grim shell
	// --name`). If blank (after trimming whitespace), a name is generated
	// automatically. Returns ErrNameTooLong if name exceeds MaxPaneNameLength.
	UpsertPane(ctx context.Context, paneID string, oplogID int, oplogName string, createdAt int64, width, height int, name string) error
	CreatePane(ctx context.Context, paneID string, oplogID int, oplogName string, createdAt int64, width, height int) error
	GetPane(ctx context.Context, paneID string) (*Pane, error)
	ListPanes(ctx context.Context) ([]*Pane, error)
	// DeregisterPane removes the pending short ID reservation for paneID so
	// the ID is immediately available to other panes. The pane row and all
	// its commands are retained for historical purposes.
	DeregisterPane(ctx context.Context, paneID string) error
	// DeletePane atomically deletes a pane, its pending reservation, and all
	// commands and command-owned records. Returns ErrNotFound when paneID does
	// not identify a pane.
	DeletePane(ctx context.Context, paneID string) error

	// Commands
	// ReserveShortID atomically claims shortID for paneID in the
	// pending_commands table. Returns ErrConflict if shortID is already in use
	// in either pending_commands or commands. Returns ErrNotFound if paneID has
	// not been registered via UpsertPane. The same pane may call this again
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
	GetCommandsByPaneSeqRange(ctx context.Context, paneID string, firstSeq, lastSeq int64) ([]*Command, error)
	// ListUnsynced returns finalized commands that have never been pushed or
	// whose metadata revision is newer than the last successful push.
	ListUnsynced(ctx context.Context) ([]*Command, error)
	// DeleteCommandByShortID atomically deletes a command and all of its
	// output chunks, tags, and metadata. When it was the pane's final command,
	// the empty pane and pending reservation are deleted as well. Returns
	// ErrNotFound when shortID does not identify a command.
	DeleteCommandByShortID(ctx context.Context, shortID string) (*DeleteCommandResult, error)
	FinalizeCommand(ctx context.Context, shortID string, endedAt int64, exitCode int) error
	// MarkCommandSynced records the successful remote oplog and syncRevision.
	// A stale completion moves the stored revision backward so the latest local
	// metadata remains pending and is retried.
	MarkCommandSynced(ctx context.Context, commandID int64, oplogID int, syncRevision int64) error
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
	RemoveTag(ctx context.Context, commandID int64, label string) error
	ListAllTags(ctx context.Context) ([]*Tag, error)
	ListAllTagsWithCommands(ctx context.Context) ([]*TagWithCommand, error)

	// Metadata
	GetCommandMetadata(ctx context.Context, commandID int64) (*CommandMetadata, error)
	// UpsertCommandMetadata atomically saves metadata and advances the command's
	// local sync revision so an existing Ghostwriter entry will be refreshed.
	UpsertCommandMetadata(ctx context.Context, m *CommandMetadata) error

	// Lifecycle
	Close() error
}
