package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	require.NoError(t, s.CreatePane(ctx, "pane-001", 0, "", 1000, 220, 50))

	// Duplicate should fail (PRIMARY KEY constraint).
	assert.Error(t, s.CreatePane(ctx, "pane-001", 0, "", 2000, 220, 50), "expected error on duplicate pane id")
}

func TestUpsertPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// First call creates the row.
	require.NoError(t, s.UpsertPane(ctx, "pane-001", 0, "", 1000, 220, 50, ""))

	// Second call (daemon restart scenario) must not error.
	require.NoError(t, s.UpsertPane(ctx, "pane-001", 0, "", 2000, 220, 50, ""), "upsert on existing pane must succeed")
}

func TestUpsertPane_CustomName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t.Run("uses_supplied_name_verbatim", func(t *testing.T) {
		require.NoError(t, s.UpsertPane(ctx, "pane-named-1", 0, "", 1000, 220, 50, "my-pane"))

		p, err := s.GetPane(ctx, "pane-named-1")
		require.NoError(t, err)
		assert.Equal(t, "my-pane", p.Name)
	})

	t.Run("blank_name_falls_back_to_auto_generated", func(t *testing.T) {
		require.NoError(t, s.UpsertPane(ctx, "pane-named-2", 0, "", 1000, 220, 50, "   "))

		p, err := s.GetPane(ctx, "pane-named-2")
		require.NoError(t, err)
		assert.Regexp(t, `^[a-z]+-[a-z]+-`, p.Name, "blank/whitespace name must fall back to auto-generated name")
	})

	t.Run("name_at_max_length_is_accepted", func(t *testing.T) {
		name := strings.Repeat("a", db.MaxPaneNameLength)
		require.NoError(t, s.UpsertPane(ctx, "pane-named-3", 0, "", 1000, 220, 50, name))

		p, err := s.GetPane(ctx, "pane-named-3")
		require.NoError(t, err)
		assert.Equal(t, name, p.Name)
	})

	t.Run("name_over_max_length_returns_ErrNameTooLong", func(t *testing.T) {
		name := strings.Repeat("a", db.MaxPaneNameLength+1)
		err := s.UpsertPane(ctx, "pane-named-4", 0, "", 1000, 220, 50, name)
		require.ErrorIs(t, err, db.ErrNameTooLong)

		_, getErr := s.GetPane(ctx, "pane-named-4")
		assert.ErrorIs(t, getErr, db.ErrNotFound, "pane must not be created when the name is rejected")
	})
}

func TestListPanes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t.Run("empty_returns_nil_slice", func(t *testing.T) {
		panes, err := s.ListPanes(ctx)
		require.NoError(t, err)
		assert.Nil(t, panes)
	})

	t.Run("returns_all_panes_newest_first_with_name", func(t *testing.T) {
		require.NoError(t, s.CreatePane(ctx, "pane-older0000001", 0, "", 1000, 220, 50))
		require.NoError(t, s.CreatePane(ctx, "pane-newer0000002", 0, "", 2000, 220, 50))

		panes, err := s.ListPanes(ctx)
		require.NoError(t, err)
		require.Len(t, panes, 2)
		assert.Equal(t, "pane-newer0000002", panes[0].ID, "newest pane must come first")
		assert.NotEmpty(t, panes[0].Name)
		assert.True(t, strings.HasSuffix(panes[0].Name, "0002"))
		assert.NotEmpty(t, panes[1].Name)
		assert.True(t, strings.HasSuffix(panes[1].Name, "0001"))
	})
}

func TestPaneName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t.Run("generated_on_create_and_ends_with_last_4_hex_chars", func(t *testing.T) {
		paneID := "a1b2c3d4e5f60718"
		require.NoError(t, s.CreatePane(ctx, paneID, 0, "", 1000, 220, 50))

		p, err := s.GetPane(ctx, paneID)
		require.NoError(t, err)
		assert.NotEmpty(t, p.Name, "pane name must be generated")
		assert.True(t, strings.HasSuffix(p.Name, "0718"), "name %q must end with the pane id's last 4 hex chars", p.Name)
		assert.Regexp(t, `^[a-z]+-[a-z]+-0718$`, p.Name, "name must be adjective-noun-suffix")
	})

	t.Run("stable_across_upsert_reregistration", func(t *testing.T) {
		paneID := "b2c3d4e5f6071829"
		require.NoError(t, s.UpsertPane(ctx, paneID, 0, "", 1000, 220, 50, ""))
		first, err := s.GetPane(ctx, paneID)
		require.NoError(t, err)

		// Simulated daemon restart: shell re-registers with the same pane ID.
		require.NoError(t, s.UpsertPane(ctx, paneID, 0, "", 2000, 220, 50, ""))
		second, err := s.GetPane(ctx, paneID)
		require.NoError(t, err)

		assert.Equal(t, first.Name, second.Name, "name must not change on re-registration")
	})

	t.Run("distinct_pane_ids_can_share_word_pair_but_not_full_name", func(t *testing.T) {
		// Regression guard: the suffix must vary with the pane id so two panes
		// never collide even if haikunator picks the same adjective/noun pair.
		require.NoError(t, s.CreatePane(ctx, "0000000000001111", 0, "", 1000, 220, 50))
		require.NoError(t, s.CreatePane(ctx, "0000000000002222", 0, "", 1000, 220, 50))

		p1, err := s.GetPane(ctx, "0000000000001111")
		require.NoError(t, err)
		p2, err := s.GetPane(ctx, "0000000000002222")
		require.NoError(t, err)

		assert.NotEqual(t, p1.Name, p2.Name)
		assert.True(t, strings.HasSuffix(p1.Name, "1111"))
		assert.True(t, strings.HasSuffix(p2.Name, "2222"))
	})
}

// --- Commands ---

func TestCreateCommand(t *testing.T) {
	t.Run("sequence_increments", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

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
			require.NoError(t, s.ReserveShortID(ctx, "pane-A", tc.shortID))
			cmd := &db.Command{PaneID: "pane-A", Command: tc.command, StartedAt: 1000}
			require.NoError(t, s.CreateCommand(ctx, cmd), "CreateCommand %q", tc.shortID)
			assert.Equal(t, tc.wantSeq, cmd.Seq, "seq for short_id=%s", tc.shortID)
			assert.NotZero(t, cmd.ID, "ID not populated for short_id=%s", tc.shortID)
			assert.Equal(t, tc.shortID, cmd.ShortID, "ShortID not populated for short_id=%s", tc.shortID)
		}
	})

	t.Run("sequence_isolated_per_pane", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		for _, pane := range []string{"pane-X", "pane-Y"} {
			require.NoError(t, s.CreatePane(ctx, pane, 0, "", 1000, 220, 50))
		}

		require.NoError(t, s.ReserveShortID(ctx, "pane-X", "xxx0001"))
		cmdX := &db.Command{PaneID: "pane-X", Command: "ls", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmdX))

		require.NoError(t, s.ReserveShortID(ctx, "pane-Y", "yyy0001"))
		cmdY := &db.Command{PaneID: "pane-Y", Command: "pwd", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmdY))

		// Both panes should start at seq=1, not share a counter.
		assert.Equal(t, int64(1), cmdX.Seq, "pane-X seq")
		assert.Equal(t, int64(1), cmdY.Seq, "pane-Y seq")
	})

	t.Run("duplicate_short_id", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

		// Reserve, create — short_id is now permanently in commands.
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "dup0001"))
		require.NoError(t, s.CreateCommand(ctx, &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}))

		// Same pane trying to reserve the same short_id again must fail.
		assert.ErrorIs(t, s.ReserveShortID(ctx, "pane-A", "dup0001"), db.ErrConflict)
	})

	t.Run("no_reservation_returns_not_found", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

		// CreateCommand without a prior ReserveShortID must fail.
		err := s.CreateCommand(ctx, &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000})
		require.ErrorIs(t, err, db.ErrNotFound)
	})

	t.Run("foreign_key_enforced", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()

		// ReserveShortID itself should fail — no pane registered.
		err := s.ReserveShortID(ctx, "no-such-pane", "fk00001")
		assert.Error(t, err, "expected FK violation for unknown pane_id")
	})
}

func TestGetCommandByShortID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "abc1234"))
	cmd := &db.Command{PaneID: "pane-A", Command: "uname -a", StartedAt: 5000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("found", func(t *testing.T) {
		got, err := s.GetCommandByShortID(ctx, "abc1234")
		require.NoError(t, err)
		assert.Equal(t, "uname -a", got.Command)
		assert.False(t, got.SyncedGW, "SyncedGW should be false on a new command")
	})

	t.Run("not_found", func(t *testing.T) {
		_, err := s.GetCommandByShortID(ctx, "missing")
		require.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestFinalizeCommand(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "fin0001"))
	cmd := &db.Command{PaneID: "pane-A", Command: "sleep 1", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("success", func(t *testing.T) {
		require.NoError(t, s.FinalizeCommand(ctx, "fin0001", 2000, 0))

		got, err := s.GetCommandByShortID(ctx, "fin0001")
		require.NoError(t, err)
		require.NotNil(t, got.EndedAt)
		assert.Equal(t, int64(2000), *got.EndedAt)
		require.NotNil(t, got.ExitCode)
		assert.Equal(t, 0, *got.ExitCode)
	})

	t.Run("not_found", func(t *testing.T) {
		err := s.FinalizeCommand(ctx, "ghost", 9999, 1)
		require.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestMarkCommandSynced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "syn0001"))
	cmd := &db.Command{PaneID: "pane-A", Command: "id", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	t.Run("success", func(t *testing.T) {
		require.NoError(t, s.MarkCommandSynced(ctx, cmd.ID))

		got, err := s.GetCommandByShortID(ctx, "syn0001")
		require.NoError(t, err)
		assert.True(t, got.SyncedGW, "SyncedGW should be true after sync")
	})

	t.Run("not_found", func(t *testing.T) {
		err := s.MarkCommandSynced(ctx, 99999)
		require.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestListCommandsByPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
	require.NoError(t, s.CreatePane(ctx, "pane-B", 0, "", 1000, 220, 50))

	cmds := []*db.Command{
		{PaneID: "pane-A", Command: "whoami", StartedAt: 1000},
		{PaneID: "pane-A", Command: "id", StartedAt: 2000},
		{PaneID: "pane-B", Command: "hostname", StartedAt: 3000},
	}
	reservations := []struct{ pane, id string }{
		{"pane-A", "lp00001"},
		{"pane-A", "lp00002"},
		{"pane-B", "lp00003"},
	}
	for i, cmd := range cmds {
		require.NoError(t, s.ReserveShortID(ctx, reservations[i].pane, reservations[i].id))
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
		require.NoError(t, s.CreatePane(ctx, "pane-empty", 0, "", 1000, 220, 50))
		got, err := s.ListCommandsByPane(ctx, "pane-empty")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestListUnsynced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

	// cmd1: finalized, unsynced — should appear.
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "u000001"))
	cmd1 := &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd1))
	require.NoError(t, s.FinalizeCommand(ctx, "u000001", 1100, 0))

	// cmd2: not finalized — should NOT appear.
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "u000002"))
	cmd2 := &db.Command{PaneID: "pane-A", Command: "top", StartedAt: 1200}
	require.NoError(t, s.CreateCommand(ctx, cmd2))

	// cmd3: finalized and synced — should NOT appear.
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "u000003"))
	cmd3 := &db.Command{PaneID: "pane-A", Command: "pwd", StartedAt: 1300}
	require.NoError(t, s.CreateCommand(ctx, cmd3))
	require.NoError(t, s.FinalizeCommand(ctx, "u000003", 1400, 0))
	require.NoError(t, s.MarkCommandSynced(ctx, cmd3.ID))

	unsynced, err := s.ListUnsynced(ctx)
	require.NoError(t, err)
	require.Len(t, unsynced, 1)
	assert.Equal(t, "u000001", unsynced[0].ShortID)
}

func TestShortIDExists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

	// "exists1" permanently in commands.
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "exists1"))
	require.NoError(t, s.CreateCommand(ctx, &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}))

	// "pending1" reserved but not yet inserted.
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "pending1"))

	cases := []struct {
		shortID string
		want    bool
	}{
		{"exists1", true},
		{"pending1", true},
		{"missing", false},
	}
	for _, tc := range cases {
		got, err := s.ShortIDExists(ctx, tc.shortID)
		require.NoError(t, err, "ShortIDExists(%q)", tc.shortID)
		assert.Equal(t, tc.want, got, "ShortIDExists(%q)", tc.shortID)
	}
}

func TestDeregisterPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "dreg001"))

	exists, err := s.ShortIDExists(ctx, "dreg001")
	require.NoError(t, err)
	assert.True(t, exists, "short_id should be reserved before deregister")

	require.NoError(t, s.DeregisterPane(ctx, "pane-A"))

	// Reservation is gone; the pane row itself must remain (historical record).
	exists, err = s.ShortIDExists(ctx, "dreg001")
	require.NoError(t, err)
	assert.False(t, exists, "short_id should be unreserved after deregister")

	// Calling DeregisterPane on a pane with no reservation must not error.
	require.NoError(t, s.DeregisterPane(ctx, "pane-A"), "idempotent deregister")
}

func TestOplogIDPropagation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-A", 42, "HUNTING_BASILISK_Oplog", 1000, 220, 50))
	require.NoError(t, s.ReserveShortID(ctx, "pane-A", "prj0001"))
	cmd := &db.Command{PaneID: "pane-A", Command: "whoami", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd))

	assert.Equal(t, 42, cmd.OplogID, "oplog_id should be propagated to in-memory struct")
	assert.Equal(t, "HUNTING_BASILISK_Oplog", cmd.OplogName, "oplog_name should be propagated to in-memory struct")

	got, err := s.GetCommandByShortID(ctx, "prj0001")
	require.NoError(t, err)
	assert.Equal(t, 42, got.OplogID, "oplog_id should be stored and retrieved from DB")
	assert.Equal(t, "HUNTING_BASILISK_Oplog", got.OplogName, "oplog_name should be stored and retrieved from DB")
}

// --- ReserveShortID ---

func TestReserveShortID(t *testing.T) {
	t.Run("conflict_with_committed_command", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "col0001"))
		require.NoError(t, s.CreateCommand(ctx, &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}))

		// short_id is now permanently in commands — reservation must fail.
		assert.ErrorIs(t, s.ReserveShortID(ctx, "pane-A", "col0001"), db.ErrConflict)
	})

	t.Run("conflict_with_other_pane_pending", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		for _, p := range []string{"pane-A", "pane-B"} {
			require.NoError(t, s.CreatePane(ctx, p, 0, "", 1000, 220, 50))
		}

		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "shared1"))

		// A different pane cannot steal the same short_id.
		assert.ErrorIs(t, s.ReserveShortID(ctx, "pane-B", "shared1"), db.ErrConflict)
	})

	t.Run("same_pane_can_refresh", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))

		// First reservation.
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "old0001"))
		// Same pane replaces it with a new short_id.
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "new0001"))

		// CreateCommand should pick up the latest reservation.
		cmd := &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmd))
		assert.Equal(t, "new0001", cmd.ShortID)
	})

	t.Run("foreign_key_on_pane", func(t *testing.T) {
		s := openTestStore(t)
		err := s.ReserveShortID(context.Background(), "no-such-pane", "fk00001")
		assert.ErrorIs(t, err, db.ErrNotFound, "unregistered pane should return ErrNotFound")
	})
}

// --- Output Chunks ---

func TestOutputChunks(t *testing.T) {
	t.Run("append_and_get_in_order", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()

		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "out0001"))
		cmd := &db.Command{PaneID: "pane-A", Command: "ls -la", StartedAt: 1000}
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

		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "emp0001"))
		cmd := &db.Command{PaneID: "pane-A", Command: "ls", StartedAt: 1000}
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

		require.NoError(t, s.CreatePane(ctx, "pane-A", 0, "", 1000, 220, 50))
		require.NoError(t, s.ReserveShortID(ctx, "pane-A", "tag0001"))
		cmd := &db.Command{PaneID: "pane-A", Command: "net view", StartedAt: 1000}
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
		// FK violations bypass INSERT OR IGNORE (SQLite ON CONFLICT does not cover
		// foreign keys), so this must be a real DB error, not ErrDuplicateTag.
		require.Error(t, err, "expected FK violation for unknown command_id")
		assert.NotErrorIs(t, err, db.ErrDuplicateTag, "FK error must not be misclassified as duplicate tag")
	})

	t.Run("duplicate_label", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()

		require.NoError(t, s.CreatePane(ctx, "pane-B", 0, "", 1000, 220, 50))
		require.NoError(t, s.ReserveShortID(ctx, "pane-B", "dup0001"))
		cmd := &db.Command{PaneID: "pane-B", Command: "whoami", StartedAt: 1000}
		require.NoError(t, s.CreateCommand(ctx, cmd))

		require.NoError(t, s.AddTag(ctx, cmd.ID, "Credential Access", 2000))
		err := s.AddTag(ctx, cmd.ID, "Credential Access", 3000)
		assert.ErrorIs(t, err, db.ErrDuplicateTag, "second identical label must return ErrDuplicateTag")
	})
}

// --- Migrations ---

func TestMigrations(t *testing.T) {
	latest := db.LatestVersionForTest()

	t.Run("fresh_db_at_latest_version", func(t *testing.T) {
		s := openTestStore(t)
		ver, err := db.UserVersionForTest(s)
		require.NoError(t, err)
		require.Equal(t, latest, ver, "fresh DB must reach the latest migration version")
	})

	t.Run("idempotent_reopen", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.db")

		// First open: applies schema + migrations.
		s1, err := db.Open(path)
		require.NoError(t, err)
		ver1, err := db.UserVersionForTest(s1)
		require.NoError(t, err)
		require.NoError(t, s1.Close())

		// Second open: migrations already applied — must not error or change version.
		s2, err := db.Open(path)
		require.NoError(t, err)
		ver2, err := db.UserVersionForTest(s2)
		require.NoError(t, err)
		require.NoError(t, s2.Close())

		require.Equal(t, ver1, ver2, "re-opening must not change user_version")
		require.Equal(t, latest, ver2)
	})

	t.Run("skips_already_applied", func(t *testing.T) {
		// Simulate a DB that was already at version latest-1 by opening a
		// fresh file, then directly setting user_version one step back and
		// re-opening — the migration must advance it to latest again without error.
		if latest < 1 {
			t.Skip("no migrations to skip")
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "test.db")

		// Bootstrap at latest (normal open).
		s1, err := db.Open(path)
		require.NoError(t, err)
		require.NoError(t, s1.Close())

		// Wind back to latest-1 so one migration is pending on re-open.
		rawDB, err := db.OpenRawForTest(path)
		require.NoError(t, err)
		_, err = rawDB.Exec("PRAGMA user_version = " + itoa(latest-1))
		require.NoError(t, err, "set user_version to latest-1")
		require.NoError(t, rawDB.Close())

		// Re-open: should apply the pending migration and advance to latest.
		s2, err := db.Open(path)
		require.NoError(t, err)
		ver, err := db.UserVersionForTest(s2)
		require.NoError(t, err)
		require.NoError(t, s2.Close())

		require.Equal(t, latest, ver, "pending migration must be applied on re-open")
	})

	t.Run("db_file_created", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "new.db")
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err), "file should not exist before Open")

		s, err := db.Open(path)
		require.NoError(t, err)
		require.NoError(t, s.Close())

		_, err = os.Stat(path)
		require.NoError(t, err, "Open must create the DB file")
	})
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// --- GetPane ---

func TestGetPane(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t.Run("not_found", func(t *testing.T) {
		_, err := s.GetPane(ctx, "missing-pane")
		require.ErrorIs(t, err, db.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		require.NoError(t, s.CreatePane(ctx, "pane-get-001", 99, "Pentest Oplog", 5000, 190, 40))
		p, err := s.GetPane(ctx, "pane-get-001")
		require.NoError(t, err)
		assert.Equal(t, "pane-get-001", p.ID)
		assert.Equal(t, 99, p.OplogID)
		assert.Equal(t, "Pentest Oplog", p.OplogName)
		assert.Equal(t, int64(5000), p.CreatedAt)
		assert.Equal(t, 190, p.Width)
		assert.Equal(t, 40, p.Height)
	})

	t.Run("defaults_on_migration", func(t *testing.T) {
		// Panes created with default width/height (220/50) must be retrievable.
		require.NoError(t, s.CreatePane(ctx, "pane-defaults", 0, "", 1000, 220, 50))
		p, err := s.GetPane(ctx, "pane-defaults")
		require.NoError(t, err)
		assert.Equal(t, 220, p.Width)
		assert.Equal(t, 50, p.Height)
	})
}

// --- GetCommandsByPaneSeqRange ---

func TestGetCommandsByPaneSeqRange(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.CreatePane(ctx, "pane-seq", 0, "", 1000, 220, 50))

	require.NoError(t, s.ReserveShortID(ctx, "pane-seq", "seq0001"))
	cmd1 := &db.Command{PaneID: "pane-seq", Command: "ls", StartedAt: 1000}
	require.NoError(t, s.CreateCommand(ctx, cmd1))

	require.NoError(t, s.ReserveShortID(ctx, "pane-seq", "seq0002"))
	cmd2 := &db.Command{PaneID: "pane-seq", Command: "pwd", StartedAt: 2000}
	require.NoError(t, s.CreateCommand(ctx, cmd2))

	require.NoError(t, s.ReserveShortID(ctx, "pane-seq", "seq0003"))
	cmd3 := &db.Command{PaneID: "pane-seq", Command: "whoami", StartedAt: 3000}
	require.NoError(t, s.CreateCommand(ctx, cmd3))

	t.Run("full_range", func(t *testing.T) {
		cmds, err := s.GetCommandsByPaneSeqRange(ctx, "pane-seq", cmd1.Seq, cmd3.Seq)
		require.NoError(t, err)
		require.Len(t, cmds, 3)
		assert.Equal(t, "ls", cmds[0].Command)
		assert.Equal(t, "whoami", cmds[2].Command)
	})

	t.Run("partial_range", func(t *testing.T) {
		cmds, err := s.GetCommandsByPaneSeqRange(ctx, "pane-seq", cmd2.Seq, cmd3.Seq)
		require.NoError(t, err)
		require.Len(t, cmds, 2)
		assert.Equal(t, "pwd", cmds[0].Command)
	})

	t.Run("single", func(t *testing.T) {
		cmds, err := s.GetCommandsByPaneSeqRange(ctx, "pane-seq", cmd2.Seq, cmd2.Seq)
		require.NoError(t, err)
		require.Len(t, cmds, 1)
		assert.Equal(t, "pwd", cmds[0].Command)
	})

	t.Run("empty_range", func(t *testing.T) {
		cmds, err := s.GetCommandsByPaneSeqRange(ctx, "pane-seq", 9999, 9999)
		require.NoError(t, err)
		require.Empty(t, cmds)
	})

	t.Run("wrong_pane", func(t *testing.T) {
		cmds, err := s.GetCommandsByPaneSeqRange(ctx, "other-pane", cmd1.Seq, cmd3.Seq)
		require.NoError(t, err)
		require.Empty(t, cmds)
	})
}

// --- Migration v2/v3 width/height ---

func TestMigrationV2V3WidthHeight(t *testing.T) {
	// Verify migration history: a fresh DB is created at the latest version,
	// which includes width/height columns in panes.
	dir := t.TempDir()
	path := filepath.Join(dir, "migration_v2v3.db")

	s, err := db.Open(path)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck

	ver, err := db.UserVersionForTest(s)
	require.NoError(t, err)
	require.GreaterOrEqual(t, ver, 3, "fresh DB must be at version 3+")

	ctx := context.Background()
	require.NoError(t, s.CreatePane(ctx, "pane-m3", 0, "", 1000, 160, 48))
	p, err := s.GetPane(ctx, "pane-m3")
	require.NoError(t, err)
	assert.Equal(t, 160, p.Width)
	assert.Equal(t, 48, p.Height)
}

func TestMigrationV10PaneNameBackfill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migration_v10.db")

	// Bootstrap at latest (name column exists, default '').
	s1, err := db.Open(path)
	require.NoError(t, err)

	// Insert a pane directly via raw SQL with name left at its default empty
	// string, simulating a row that existed before this migration ran (the
	// column exists — same as a real upgrade — but no name was ever backfilled).
	rawDB, err := db.OpenRawForTest(path)
	require.NoError(t, err)
	_, err = rawDB.Exec(
		`INSERT INTO panes (id, oplog_id, oplog_name, created_at, width, height, name) VALUES (?, 0, '', 1000, 220, 50, '')`,
		"legacy-pane-0000000000abcd",
	)
	require.NoError(t, err, "insert legacy pane row")
	_, err = rawDB.Exec("PRAGMA user_version = 9")
	require.NoError(t, err, "wind back to pre-migration-10 version")
	require.NoError(t, rawDB.Close())
	require.NoError(t, s1.Close())

	// Re-open: migration 10 must backfill the empty name.
	s2, err := db.Open(path)
	require.NoError(t, err)
	defer s2.Close() //nolint:errcheck

	p, err := s2.GetPane(context.Background(), "legacy-pane-0000000000abcd")
	require.NoError(t, err)
	assert.NotEmpty(t, p.Name, "legacy pane must have a backfilled name")
	assert.True(t, strings.HasSuffix(p.Name, "abcd"), "backfilled name %q must end with last 4 chars of id", p.Name)
}
