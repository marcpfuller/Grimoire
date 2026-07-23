package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/db"
)

// openTestStore opens an in-memory SQLite store for the duration of a test.
func openTestStore(t *testing.T) db.Store {
	t.Helper()
	s, err := db.Open(":memory:")
	require.NoError(t, err, "open test store")
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Panes ---

func TestCreatePane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-001", 1000))

	// Duplicate should fail (PRIMARY KEY constraint).
	assert.Error(t, s.CreatePane(ctx, "pane-001", 2000), "expected error on duplicate pane id")
}

// --- Commands ---

func TestCreateCommand_SequenceIncrements(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))

	cases := []struct {
		shortID string
		command string
		wantSeq int64
	}{
		{"aaa0001", "whoami", 1},
		{"aaa0002", "id", 2},
		{"aaa0003", "hostname", 3},
	}

	for _, tc := range cases {
		cmd := &db.Command{
			ShortID:   tc.shortID,
			PaneID:    "pane-A",
			Command:   tc.command,
			StartedAt: 1000,
		}
		require.NoError(t, s.CreateCommand(ctx, cmd), "CreateCommand %q", tc.shortID)
		assert.Equal(t, tc.wantSeq, cmd.Seq, "seq for short_id=%s", tc.shortID)
		assert.NotZero(t, cmd.ID, "ID not populated for short_id=%s", tc.shortID)
	}
}

func TestCreateCommand_SequenceIsolatedPerPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, pane := range []string{"pane-X", "pane-Y"} {
		require.NoError(t, s.CreatePane(ctx, pane, 1000))
	}

	cmdX := &db.Command{ShortID: "xxx0001", PaneID: "pane-X", Command: "ls", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmdX))

	cmdY := &db.Command{ShortID: "yyy0001", PaneID: "pane-Y", Command: "pwd", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmdY))

	// Both panes should start at seq=1, not share a counter.
	assert.Equal(t, int64(1), cmdX.Seq, "pane-X seq")
	assert.Equal(t, int64(1), cmdY.Seq, "pane-Y seq")
}

func TestGetCommandByShortID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "abc1234", PaneID: "pane-A", Command: "uname -a", StartedAt: 5000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	got, err := s.GetCommandByShortID(ctx, "abc1234")
	require.NoError(t, err)
	assert.Equal(t, "uname -a", got.Command)
	assert.False(t, got.SyncedGW, "SyncedGW should be false on a new command")
}

func TestGetCommandByShortID_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetCommandByShortID(context.Background(), "missing")
	assert.ErrorIs(t, err, db.ErrNotFound)
}

func TestFinalizeCommand(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "fin0001", PaneID: "pane-A", Command: "sleep 1", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	require.NoError(t, s.FinalizeCommand(ctx, "fin0001", 2000))

	got, err := s.GetCommandByShortID(ctx, "fin0001")
	require.NoError(t, err)
	require.NotNil(t, got.EndedAt)
	assert.Equal(t, int64(2000), *got.EndedAt)
}

func TestFinalizeCommand_NotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.FinalizeCommand(context.Background(), "ghost", 9999)
	assert.ErrorIs(t, err, db.ErrNotFound)
}

func TestMarkCommandSynced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "syn0001", PaneID: "pane-A", Command: "id", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	require.NoError(t, s.MarkCommandSynced(ctx, cmd.ID))

	got, err := s.GetCommandByShortID(ctx, "syn0001")
	require.NoError(t, err)
	assert.True(t, got.SyncedGW, "SyncedGW should be true after sync")
}

func TestListUnsynced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))

	// cmd1: finalized, unsynced — should appear.
	cmd1 := &db.Command{ShortID: "u000001", PaneID: "pane-A", Command: "ls", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd1))
	require.NoError(t, s.FinalizeCommand(ctx, "u000001", 1100))

	// cmd2: not finalized — should NOT appear.
	cmd2 := &db.Command{ShortID: "u000002", PaneID: "pane-A", Command: "top", StartedAt: 1200}
	require.NoError(t, s.CreateCommand(ctx, cmd2))

	// cmd3: finalized and synced — should NOT appear.
	cmd3 := &db.Command{ShortID: "u000003", PaneID: "pane-A", Command: "pwd", StartedAt: 1300}
	require.NoError(t, s.CreateCommand(ctx, cmd3))
	require.NoError(t, s.FinalizeCommand(ctx, "u000003", 1400))
	require.NoError(t, s.MarkCommandSynced(ctx, cmd3.ID))

	unsynced, err := s.ListUnsynced(ctx)
	require.NoError(t, err)
	require.Len(t, unsynced, 1)
	assert.Equal(t, "u000001", unsynced[0].ShortID)
}

func TestShortIDExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "exists1", PaneID: "pane-A", Command: "ls", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	cases := []struct {
		shortID string
		want    bool
	}{
		{"exists1", true},
		{"missing", false},
	}
	for _, tc := range cases {
		got, err := s.ShortIDExists(ctx, tc.shortID)
		require.NoError(t, err, "ShortIDExists(%q)", tc.shortID)
		assert.Equal(t, tc.want, got, "ShortIDExists(%q)", tc.shortID)
	}
}

// --- Output Chunks ---

func TestAppendAndGetOutputChunks(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "out0001", PaneID: "pane-A", Command: "ls -la", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	chunks := []struct {
		offsetMs int64
		data     []byte
	}{
		{0, []byte("total 48\n")},
		{12, []byte("drwxr-xr-x  5 root\n")},
		{25, []byte("-rw-r--r--  1 root\n")},
	}

	for _, c := range chunks {
		chunk := &db.OutputChunk{CommandID: cmd.ID, OffsetMs: c.offsetMs, Data: c.data}
		require.NoError(t, s.AppendOutputChunk(ctx, chunk), "AppendOutputChunk offset=%d", c.offsetMs)
		assert.NotZero(t, chunk.ID, "chunk ID not populated after insert")
	}

	got, err := s.GetOutputChunks(ctx, cmd.ID)
	require.NoError(t, err)
	require.Len(t, got, len(chunks))

	// Verify returned in offset order.
	for i, c := range got {
		assert.Equal(t, chunks[i].offsetMs, c.OffsetMs, "chunk[%d] OffsetMs", i)
		assert.Equal(t, chunks[i].data, c.Data, "chunk[%d] Data", i)
	}
}

// --- Tags ---

func TestAddAndGetTags(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "tag0001", PaneID: "pane-A", Command: "net view", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	labels := []string{"Enumerated shares", "Initial access"}
	for i, label := range labels {
		require.NoError(t, s.AddTag(ctx, cmd.ID, label, int64(2000+i)), "AddTag %q", label)
	}

	tags, err := s.GetTagsByCommand(ctx, cmd.ID)
	require.NoError(t, err)
	require.Len(t, tags, len(labels))
	for i, tag := range tags {
		assert.Equal(t, labels[i], tag.Label, "tag[%d] Label", i)
	}
}

