package cast_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/cast"
	"github.com/marcpfuller/grimoire/internal/db"
	dbmocks "github.com/marcpfuller/grimoire/internal/db/mocks"
)

// --- helper to parse .cast output ---

type castHeader struct {
	Version   int    `json:"version"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"`
}

type castEvent struct {
	Time float64
	Type string
	Data string
}

func parseCast(t *testing.T, path string) (castHeader, []castEvent) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)

	require.True(t, scanner.Scan(), "cast file must have header line")
	var hdr castHeader
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &hdr))

	var events []castEvent
	for scanner.Scan() {
		var raw [3]json.RawMessage
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &raw))
		var ev castEvent
		require.NoError(t, json.Unmarshal(raw[0], &ev.Time))
		require.NoError(t, json.Unmarshal(raw[1], &ev.Type))
		require.NoError(t, json.Unmarshal(raw[2], &ev.Data))
		events = append(events, ev)
	}
	require.NoError(t, scanner.Err())
	return hdr, events
}

// --- tests ---

func TestSynthesizeHappyPath(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd1 := &db.Command{ID: 1, ShortID: "abc0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	cmd2 := &db.Command{ID: 2, ShortID: "abc0002", PaneID: "pane-1", Seq: 2, Command: "pwd", StartedAt: 3500}
	pane := &db.Pane{ID: "pane-1", OplogID: 1, OplogName: "proj-1 Oplog", Width: 200, Height: 50}
	chunks1 := []*db.OutputChunk{
		{CommandID: 1, Data: []byte("file1.txt\n"), OffsetMs: 100},
		{CommandID: 1, Data: []byte("file2.txt\n"), OffsetMs: 200},
	}
	chunks2 := []*db.OutputChunk{
		{CommandID: 2, Data: []byte("/home/user\n"), OffsetMs: 100},
	}

	store.On("GetCommandByShortID", mock.Anything, "abc0001").Return(cmd1, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "abc0002").Return(cmd2, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(chunks1, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(2)).Return(chunks2, nil).Once()

	dir := t.TempDir()
	cfg := cast.Params{
		ShortIDs: []string{"abc0001", "abc0002"},
		OutFile:  filepath.Join(dir, "test.cast"),
	}

	path, err := cast.Synthesize(context.Background(), store, cfg)
	require.NoError(t, err)
	assert.Equal(t, cfg.OutFile, path)

	hdr, events := parseCast(t, path)
	assert.Equal(t, 2, hdr.Version)
	assert.Equal(t, 200, hdr.Width)
	assert.Equal(t, 50, hdr.Height)

	// cmd1 header + 2 chunks + cmd2 header + 1 chunk = 5 events
	require.Len(t, events, 5)

	for _, ev := range events {
		assert.Equal(t, "o", ev.Type)
	}
	assert.Contains(t, events[0].Data, "ls")

	// Timestamps are monotonically non-decreasing
	for i := 1; i < len(events); i++ {
		assert.GreaterOrEqual(t, events[i].Time, events[i-1].Time)
	}
}

func TestSynthesizeSingleID(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "sin0001", PaneID: "pane-1", Seq: 1, Command: "echo hi", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "sin0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return([]*db.OutputChunk{{CommandID: 1, Data: []byte("hi\n"), OffsetMs: 50}}, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"sin0001"},
		OutFile:  filepath.Join(t.TempDir(), "single.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.Len(t, events, 2) // command line + 1 chunk
	assert.Contains(t, events[0].Data, "echo hi")
}

func TestSynthesizeNonContiguousIDs(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	// seq 1 and seq 5 — seq 2,3,4 are skipped
	cmd1 := &db.Command{ID: 1, ShortID: "nc0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	cmd5 := &db.Command{ID: 5, ShortID: "nc0005", PaneID: "pane-1", Seq: 5, Command: "pwd", StartedAt: 9000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "nc0001").Return(cmd1, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "nc0005").Return(cmd5, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(5)).Return(nil, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"nc0001", "nc0005"},
		OutFile:  filepath.Join(t.TempDir(), "noncontig.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.Len(t, events, 2) // one bare command line per command, no chunks
	assert.Contains(t, events[0].Data, "ls")
	assert.Contains(t, events[1].Data, "pwd")
}

func TestSynthesizeCrossPane(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd1 := &db.Command{ID: 1, ShortID: "aaa0001", PaneID: "pane-A", Seq: 1, Command: "ls", StartedAt: 1000}
	cmd2 := &db.Command{ID: 2, ShortID: "bbb0001", PaneID: "pane-B", Seq: 1, Command: "pwd", StartedAt: 2000}

	store.On("GetCommandByShortID", mock.Anything, "aaa0001").Return(cmd1, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "bbb0001").Return(cmd2, nil).Once()

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"aaa0001", "bbb0001"},
		OutFile:  filepath.Join(t.TempDir(), "cross.cast"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same pane")
}

func TestSynthesizeNoOutputChunks(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "noc0001", PaneID: "pane-1", Seq: 1, Command: "clear", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "noc0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"noc0001"},
		OutFile:  filepath.Join(t.TempDir(), "nochunks.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.Len(t, events, 1, "one bare command event expected")
	assert.Contains(t, events[0].Data, "clear")
}

func TestSynthesizeGapCompression(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd1 := &db.Command{ID: 1, ShortID: "gap0001", PaneID: "pane-1", Seq: 1, Command: "sleep 10", StartedAt: 1000}
	cmd2 := &db.Command{ID: 2, ShortID: "gap0002", PaneID: "pane-1", Seq: 2, Command: "ls", StartedAt: 11000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "gap0001").Return(cmd1, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "gap0002").Return(cmd2, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return([]*db.OutputChunk{{CommandID: 1, Data: []byte("done\n"), OffsetMs: 9900}}, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(2)).Return([]*db.OutputChunk{{CommandID: 2, Data: []byte("file\n"), OffsetMs: 100}}, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"gap0001", "gap0002"},
		OutFile:  filepath.Join(t.TempDir(), "gap.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.NotEmpty(t, events)

	// Max gap between any two consecutive events must be <= 2.0 seconds
	for i := 1; i < len(events); i++ {
		gap := events[i].Time - events[i-1].Time
		assert.LessOrEqual(t, gap, 2.0, "gap between events[%d] and events[%d] should be compressed", i-1, i)
	}
}

func TestSynthesizeEmptyShortIDs(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{},
		OutFile:  filepath.Join(t.TempDir(), "empty.cast"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one")
}

func TestSynthesizeIDNotFound(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	store.On("GetCommandByShortID", mock.Anything, "nope0001").Return(nil, db.ErrNotFound).Once()

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"nope0001"},
		OutFile:  filepath.Join(t.TempDir(), "notfound.cast"),
	})
	require.ErrorIs(t, err, db.ErrNotFound)
}

func TestSynthesizeSecondIDNotFound(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd1 := &db.Command{ID: 1, ShortID: "ok0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	store.On("GetCommandByShortID", mock.Anything, "ok0001").Return(cmd1, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "nope0002").Return(nil, db.ErrNotFound).Once()

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"ok0001", "nope0002"},
		OutFile:  filepath.Join(t.TempDir(), "notfound.cast"),
	})
	require.ErrorIs(t, err, db.ErrNotFound)
}

func TestSynthesizeReversedIDs(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd1 := &db.Command{ID: 1, ShortID: "rev0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	cmd2 := &db.Command{ID: 2, ShortID: "rev0002", PaneID: "pane-1", Seq: 2, Command: "pwd", StartedAt: 2000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	// IDs supplied in reverse order — daemon must sort them.
	store.On("GetCommandByShortID", mock.Anything, "rev0002").Return(cmd2, nil).Once()
	store.On("GetCommandByShortID", mock.Anything, "rev0001").Return(cmd1, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(2)).Return(nil, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"rev0002", "rev0001"}, // reversed — should be sorted automatically
		OutFile:  filepath.Join(t.TempDir(), "reversed.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.Len(t, events, 2)
	// After sorting, ls (seq 1) must appear before pwd (seq 2).
	assert.Contains(t, events[0].Data, "ls")
	assert.Contains(t, events[1].Data, "pwd")
	// Timestamps must be non-decreasing.
	assert.GreaterOrEqual(t, events[1].Time, events[0].Time)
}

func TestSynthesizeDuplicateSeq(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	// Same command ID supplied twice — after dedup only one event should appear.
	cmd := &db.Command{ID: 1, ShortID: "dup0001", PaneID: "pane-1", Seq: 3, Command: "ls", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "dup0001").Return(cmd, nil).Times(2)
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, nil).Once()

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"dup0001", "dup0001"},
		OutFile:  filepath.Join(t.TempDir(), "dup.cast"),
	})
	require.NoError(t, err)

	_, events := parseCast(t, path)
	require.Len(t, events, 1, "duplicate should be collapsed to a single event")
	assert.Contains(t, events[0].Data, "ls")
}

func TestSynthesizeOutFile(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "out0001", PaneID: "pane-1", Seq: 1, Command: "echo hi", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "out0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return([]*db.OutputChunk{{CommandID: 1, Data: []byte("hi\n"), OffsetMs: 50}}, nil).Once()

	dir := t.TempDir()
	explicit := filepath.Join(dir, "my-session.cast")

	path, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"out0001"},
		OutFile:  explicit,
	})
	require.NoError(t, err)
	require.Equal(t, explicit, path)
	_, statErr := os.Stat(explicit)
	assert.NoError(t, statErr)
}

func TestSynthesizeDefaultOutFile(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "def0001", PaneID: "pane-1", Seq: 1, Command: "echo hi", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}

	store.On("GetCommandByShortID", mock.Anything, "def0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, nil).Once()

	dir := t.TempDir()
	cfg := cast.Params{
		ShortIDs: []string{"def0001"},
		OutDir:   dir,
		// OutFile intentionally empty: should use OutDir + timestamped name
	}

	path, err := cast.Synthesize(context.Background(), store, cfg)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(path, dir), "output path must be inside OutDir")
	assert.True(t, strings.HasSuffix(path, ".cast"), "output path must end in .cast")
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

func TestSynthesizeGetPaneError(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "pe0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	store.On("GetCommandByShortID", mock.Anything, "pe0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(nil, db.ErrNotFound).Once()

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"pe0001"},
		OutFile:  filepath.Join(t.TempDir(), "paneerr.cast"),
	})
	require.ErrorIs(t, err, db.ErrNotFound)
}

func TestSynthesizeGetOutputChunksError(t *testing.T) {
	store := dbmocks.NewMockStore(t)

	cmd := &db.Command{ID: 1, ShortID: "oc0001", PaneID: "pane-1", Seq: 1, Command: "ls", StartedAt: 1000}
	pane := &db.Pane{ID: "pane-1", Width: 220, Height: 50}
	dbErr := errors.New("storage failure")

	store.On("GetCommandByShortID", mock.Anything, "oc0001").Return(cmd, nil).Once()
	store.On("GetPane", mock.Anything, "pane-1").Return(pane, nil).Once()
	store.On("GetOutputChunks", mock.Anything, int64(1)).Return(nil, dbErr).Once()

	_, err := cast.Synthesize(context.Background(), store, cast.Params{
		ShortIDs: []string{"oc0001"},
		OutFile:  filepath.Join(t.TempDir(), "chunkserr.cast"),
	})
	require.ErrorIs(t, err, dbErr)
}
