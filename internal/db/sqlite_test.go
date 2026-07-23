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
	t.Cleanup(func() { _ = s.Close() })
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

func TestCreateCommand(t *testing.T) {
	t.Run("sequence_increments", func(t *testing.T) {
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
	})

	t.Run("sequence_isolated_per_pane", func(t *testing.T) {
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
	})

	t.Run("duplicate_short_id", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))

		cmd1 := &db.Command{ShortID: "dup0001", PaneID: "pane-A", Command: "ls", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmd1))

		cmd2 := &db.Command{ShortID: "dup0001", PaneID: "pane-A", Command: "pwd", StartedAt: 2000}
		assert.Error(t, s.CreateCommand(ctx, cmd2), "expected error on duplicate short_id")
	})

	t.Run("foreign_key_enforced", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()

		// No pane created — FK constraint must reject the insert.
		cmd := &db.Command{ShortID: "fk00001", PaneID: "no-such-pane", Command: "ls", StartedAt: 1000}
		assert.Error(t, s.CreateCommand(ctx, cmd), "expected FK violation for unknown pane_id")
	})
}

func TestGetCommandByShortID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "abc1234", PaneID: "pane-A", Command: "uname -a", StartedAt: 5000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("found", func(t *testing.T) {
		got, err := s.GetCommandByShortID(ctx, "abc1234")
		require.NoError(t, err)
		assert.Equal(t, "uname -a", got.Command)
		assert.False(t, got.SyncedGW, "SyncedGW should be false on a new command")
	})

	t.Run("not_found", func(t *testing.T) {
		_, err := s.GetCommandByShortID(ctx, "missing")
		assert.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestFinalizeCommand(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "fin0001", PaneID: "pane-A", Command: "sleep 1", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("success", func(t *testing.T) {
		require.NoError(t, s.FinalizeCommand(ctx, "fin0001", 2000))

		got, err := s.GetCommandByShortID(ctx, "fin0001")
		require.NoError(t, err)
		require.NotNil(t, got.EndedAt)
		assert.Equal(t, int64(2000), *got.EndedAt)
	})

	t.Run("not_found", func(t *testing.T) {
		err := s.FinalizeCommand(ctx, "ghost", 9999)
		assert.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestMarkCommandSynced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	cmd := &db.Command{ShortID: "syn0001", PaneID: "pane-A", Command: "id", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("success", func(t *testing.T) {
		require.NoError(t, s.MarkCommandSynced(ctx, cmd.ID))

		got, err := s.GetCommandByShortID(ctx, "syn0001")
		require.NoError(t, err)
		assert.True(t, got.SyncedGW, "SyncedGW should be true after sync")
	})

	t.Run("not_found", func(t *testing.T) {
		err := s.MarkCommandSynced(ctx, 99999)
		assert.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestListCommandsByPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
	require.NoError(t, s.CreatePane(ctx, "pane-B", 1000))

	cmds := []*db.Command{
		{ShortID: "lp00001", PaneID: "pane-A", Command: "whoami", StartedAt: 1000},
		{ShortID: "lp00002", PaneID: "pane-A", Command: "id", StartedAt: 2000},
		{ShortID: "lp00003", PaneID: "pane-B", Command: "hostname", StartedAt: 3000},
	}
	for _, cmd := range cmds {
		require.NoError(t, s.CreateCommand(ctx, cmd))
	}

	t.Run("returns_commands_in_seq_order", func(t *testing.T) {
		got, err := s.ListCommandsByPane(ctx, "pane-A")
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "whoami", got[0].Command)
		assert.Equal(t, "id", got[1].Command)
		assert.Equal(t, int64(1), got[0].Seq)
		assert.Equal(t, int64(2), got[1].Seq)
	})

	t.Run("excludes_other_panes", func(t *testing.T) {
		got, err := s.ListCommandsByPane(ctx, "pane-B")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "hostname", got[0].Command)
	})

	t.Run("empty_pane_returns_nil_slice", func(t *testing.T) {
		require.NoError(t, s.CreatePane(ctx, "pane-empty", 1000))
		got, err := s.ListCommandsByPane(ctx, "pane-empty")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
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

func TestOutputChunks(t *testing.T) {
	t.Run("append_and_get_in_order", func(t *testing.T) {
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

		for i, c := range got {
			assert.Equal(t, chunks[i].offsetMs, c.OffsetMs, "chunk[%d] OffsetMs", i)
			assert.Equal(t, chunks[i].data, c.Data, "chunk[%d] Data", i)
		}
	})

	t.Run("empty_returns_nil_slice", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()

		require.NoError(t, s.CreatePane(ctx, "pane-A", 1000))
		cmd := &db.Command{ShortID: "emp0001", PaneID: "pane-A", Command: "ls", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmd))

		chunks, err := s.GetOutputChunks(ctx, cmd.ID)
		require.NoError(t, err)
		assert.Empty(t, chunks, "expected no chunks for a command with no output")
	})
}

// --- Tags ---

func TestTags(t *testing.T) {
	t.Run("add_and_get", func(t *testing.T) {
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
	})

	t.Run("invalid_command_id", func(t *testing.T) {
		s := openTestStore(t)
		err := s.AddTag(context.Background(), 99999, "should fail", 1000)
		assert.Error(t, err, "expected FK violation for unknown command_id")
	})
}
