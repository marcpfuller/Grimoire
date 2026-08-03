package sync_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/db"
	dbmocks "github.com/marcpfuller/grimoire/internal/db/mocks"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
)

// --- test doubles ---

// mockPusher is a simple test double for grimsync.Pusher.
type mockPusher struct {
	err    error
	pushed []*db.Command
}

func (m *mockPusher) PushCommand(_ context.Context, cmd *db.Command) error {
	m.pushed = append(m.pushed, cmd)
	return m.err
}

// funcPusher allows per-command error control.
type funcPusher struct {
	fn     func(*db.Command) error
	pushed []*db.Command
}

func (f *funcPusher) PushCommand(_ context.Context, cmd *db.Command) error {
	f.pushed = append(f.pushed, cmd)
	return f.fn(cmd)
}

// --- helpers ---

func makeCmd(id int64) *db.Command {
	return &db.Command{ID: id, ProjectID: "proj1"}
}

func newSyncer(t *testing.T, store *dbmocks.MockStore, pusher grimsync.Pusher) *grimsync.Syncer {
	t.Helper()
	return grimsync.New(store, pusher, grimsync.Config{Interval: time.Second, AutoSync: true}, nil)
}

// --- RunOnce ---

func TestSyncer_RunOnce_success(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmds := []*db.Command{makeCmd(1), makeCmd(2)}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(2)).Return(nil).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Equal(t, cmds, pusher.pushed, "both commands must be pushed")
}

func TestSyncer_RunOnce_empty_batch(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{}, nil).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Empty(t, pusher.pushed, "nothing to push")
}

func TestSyncer_RunOnce_list_failure(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	store.On("ListUnsynced", mock.Anything).Return(([]*db.Command)(nil), errors.New("db down")).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Empty(t, pusher.pushed, "no push when ListUnsynced fails")
}

func TestSyncer_RunOnce_transient_push_failure(t *testing.T) {
	// Transient error (network/5xx): WARN, SyncedGW stays false — no MarkCommandSynced.
	store := dbmocks.NewMockStore(t)
	cmds := []*db.Command{makeCmd(1)}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	// MarkCommandSynced must NOT be called — testify/mock fails if unexpected calls occur.
	pusher := &mockPusher{err: errors.New("connection refused")}

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Len(t, pusher.pushed, 1, "push was attempted")
}

func TestSyncer_RunOnce_auth_push_failure(t *testing.T) {
	// AuthError: ERROR logged, no retry, no MarkCommandSynced.
	store := dbmocks.NewMockStore(t)
	cmds := []*db.Command{makeCmd(1)}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	pusher := &mockPusher{err: &grimsync.AuthError{Err: errors.New("401 unauthorized")}}

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Len(t, pusher.pushed, 1, "push was attempted")
}

func TestSyncer_RunOnce_continues_after_push_failure(t *testing.T) {
	// Best-effort: cmd 1 push fails → cmd 2 is still pushed and marked synced.
	store := dbmocks.NewMockStore(t)
	cmds := []*db.Command{makeCmd(1), makeCmd(2)}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(2)).Return(nil).Once()

	fp := &funcPusher{fn: func(cmd *db.Command) error {
		if cmd.ID == 1 {
			return errors.New("transient")
		}
		return nil
	}}

	newSyncer(t, store, fp).RunOnce(context.Background())

	require.Len(t, fp.pushed, 2)
	assert.Equal(t, int64(1), fp.pushed[0].ID)
	assert.Equal(t, int64(2), fp.pushed[1].ID)
}

func TestSyncer_RunOnce_mark_synced_failure(t *testing.T) {
	// Push succeeds but MarkCommandSynced fails — logs WARN, command retried next tick.
	store := dbmocks.NewMockStore(t)
	cmds := []*db.Command{makeCmd(1)}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(errors.New("write error")).Once()
	pusher := &mockPusher{}

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Len(t, pusher.pushed, 1)
}

// --- Run scheduling ---

func TestSyncer_Run_fires_immediately(t *testing.T) {
	// The first batch must fire before the first tick.
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	fired := make(chan struct{}, 1)
	store.On("ListUnsynced", mock.Anything).
		Run(func(_ mock.Arguments) { fired <- struct{}{} }).
		Return([]*db.Command{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Hour, AutoSync: true}, nil)

	go func() {
		s.Run(ctx)
	}()

	select {
	case <-fired:
		// good — fired before the 1h ticker
		cancel()
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("sync did not fire immediately on Run")
	}
}

func TestSyncer_Run_fires_on_tick(t *testing.T) {
	// After the immediate fire, it fires again after the interval.
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	callCount := 0
	ready := make(chan struct{})
	store.On("ListUnsynced", mock.Anything).
		Run(func(_ mock.Arguments) {
			callCount++
			if callCount == 2 {
				close(ready)
			}
		}).
		Return([]*db.Command{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := grimsync.New(store, pusher, grimsync.Config{Interval: 5 * time.Millisecond, AutoSync: true}, nil)
	go s.Run(ctx)

	select {
	case <-ready:
		// second call happened within the interval
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second sync tick did not fire")
	}
	cancel()
}

func TestSyncer_Run_drains_on_shutdown(t *testing.T) {
	// When ctx is cancelled, the in-flight batch should complete (drain).
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}

	// First call: immediate fire — slow it down a bit
	// Drain call: happens after cancel
	drainFired := make(chan struct{}, 1)
	callCount := 0
	store.On("ListUnsynced", mock.Anything).
		Run(func(_ mock.Arguments) {
			callCount++
			if callCount == 2 {
				drainFired <- struct{}{}
			}
		}).
		Return([]*db.Command{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Hour, AutoSync: true}, nil)

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Wait for immediate fire, then cancel
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-drainFired:
		// drain fired as expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("drain sync did not fire after cancel")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after drain")
	}
}

// --- AuthError ---

func TestAuthError_implements_error(t *testing.T) {
	inner := errors.New("401")
	ae := &grimsync.AuthError{Err: inner}
	require.ErrorContains(t, ae, "401")
	require.ErrorAs(t, ae, new(*grimsync.AuthError))
	require.ErrorIs(t, ae.Unwrap(), inner)
}

// --- SetInterval ---

func TestSyncer_SetInterval_updates_live_ticker(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}

	callCount := 0
	ready := make(chan struct{})
	store.On("ListUnsynced", mock.Anything).
		Run(func(_ mock.Arguments) {
			callCount++
			if callCount == 2 {
				close(ready)
			}
		}).
		Return([]*db.Command{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with a very long interval so the second tick never fires on its own.
	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Hour, AutoSync: true}, nil)
	go s.Run(ctx)

	// Wait for the immediate first fire.
	time.Sleep(20 * time.Millisecond)

	// Shorten the interval to 5 ms — the ticker should reset and fire quickly.
	s.SetInterval(5 * time.Millisecond)

	select {
	case <-ready:
		// second batch fired after interval reset
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second sync tick did not fire after SetInterval")
	}
}

func TestSyncer_SetInterval_zero_defaults_to_30s(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{}, nil).Maybe()
	s := grimsync.New(store, &mockPusher{}, grimsync.Config{Interval: time.Minute, AutoSync: true}, nil)
	s.SetInterval(0)
	assert.Equal(t, 30*time.Second, s.Interval())
}

// --- SetPusher ---

func TestSyncer_SetPusher_swaps_active_pusher(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	original := &mockPusher{}
	replacement := &mockPusher{}

	cmd := makeCmd(1)
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{cmd}, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(nil).Once()

	s := grimsync.New(store, original, grimsync.Config{Interval: time.Second, AutoSync: true}, nil)
	s.SetPusher(replacement)

	s.RunOnce(context.Background())

	require.Empty(t, original.pushed, "original pusher must not be used after swap")
	require.Len(t, replacement.pushed, 1, "replacement pusher must handle the command")
}

func TestSyncer_OnSynced_called_after_mark(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}

	cmd := makeCmd(1)
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{cmd}, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(nil).Once()

	var synced []string
	s := grimsync.New(store, pusher, grimsync.Config{
		Interval: time.Second,
		AutoSync: true,
		OnSynced: func(shortID string) { synced = append(synced, shortID) },
	}, nil)

	s.RunOnce(context.Background())

	require.Equal(t, []string{cmd.ShortID}, synced)
}

func TestSyncer_OnSynced_not_called_on_mark_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}

	cmd := makeCmd(1)
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{cmd}, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(errors.New("db error")).Once()

	var synced []string
	s := grimsync.New(store, pusher, grimsync.Config{
		Interval: time.Second,
		AutoSync: true,
		OnSynced: func(shortID string) { synced = append(synced, shortID) },
	}, nil)

	s.RunOnce(context.Background())

	require.Empty(t, synced, "OnSynced must not be called when MarkCommandSynced fails")
}

// --- AutoSync ---

func TestSyncer_AutoSync_false_skips_batch(t *testing.T) {
	// When AutoSync is false, syncBatch must not call ListUnsynced or push anything.
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	// No expectations set — any call to store or pusher will cause the mock to fail.
	s := grimsync.New(store, pusher, grimsync.Config{
		Interval: time.Second,
		AutoSync: false,
	}, nil)

	s.RunOnce(context.Background())

	require.Empty(t, pusher.pushed)
}

func TestSyncer_SetAutoSync_enables_batch(t *testing.T) {
	// After SetAutoSync(true), the next RunOnce should process commands normally.
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := makeCmd(1)
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{cmd}, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(1)).Return(nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{
		Interval: time.Second,
		AutoSync: false,
	}, nil)
	s.SetAutoSync(true)

	s.RunOnce(context.Background())

	require.Len(t, pusher.pushed, 1)
}

// --- PushOne ---

func TestSyncer_PushOne_success(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 5, ShortID: "abc1234", SyncedGW: false, ProjectID: "proj1"}

	store.On("GetCommandByShortID", mock.Anything, "abc1234").Return(cmd, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(5)).Return(nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOne(context.Background(), "abc1234")

	require.NoError(t, err)
	require.Len(t, pusher.pushed, 1)
	assert.Equal(t, int64(5), pusher.pushed[0].ID)
}

func TestSyncer_PushOne_already_synced_returns_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 5, ShortID: "abc1234", SyncedGW: true}

	store.On("GetCommandByShortID", mock.Anything, "abc1234").Return(cmd, nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOne(context.Background(), "abc1234")

	require.Error(t, err)
	require.Empty(t, pusher.pushed, "already-synced command must not be pushed again")
}

func TestSyncer_PushOne_not_found_returns_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}

	store.On("GetCommandByShortID", mock.Anything, "missing1").Return((*db.Command)(nil), db.ErrNotFound).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOne(context.Background(), "missing1")

	require.Error(t, err)
	require.Empty(t, pusher.pushed)
}

func TestSyncer_PushOne_push_failure_returns_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{err: errors.New("network error")}
	cmd := &db.Command{ID: 7, ShortID: "abc5678", SyncedGW: false, ProjectID: "proj1"}

	store.On("GetCommandByShortID", mock.Anything, "abc5678").Return(cmd, nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOne(context.Background(), "abc5678")

	require.Error(t, err)
}

func TestSyncer_PushOne_calls_OnSynced(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 9, ShortID: "def9999", SyncedGW: false, ProjectID: "proj1"}

	store.On("GetCommandByShortID", mock.Anything, "def9999").Return(cmd, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(9)).Return(nil).Once()

	var synced []string
	s := grimsync.New(store, pusher, grimsync.Config{
		Interval: time.Second,
		OnSynced: func(shortID string) { synced = append(synced, shortID) },
	}, nil)

	require.NoError(t, s.PushOne(context.Background(), "def9999"))
	assert.Equal(t, []string{"def9999"}, synced)
}

func TestSyncer_PushOne_no_project_id_returns_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 3, ShortID: "noproj1", SyncedGW: false, ProjectID: ""}

	store.On("GetCommandByShortID", mock.Anything, "noproj1").Return(cmd, nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOne(context.Background(), "noproj1")

	require.Error(t, err)
	require.ErrorIs(t, err, grimsync.ErrNoProjectID)
	require.Empty(t, pusher.pushed, "must not push command with no project ID")
}

func TestSyncer_PushOneWithProject_success(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 11, ShortID: "noproj2", SyncedGW: false, ProjectID: ""}

	store.On("GetCommandByShortID", mock.Anything, "noproj2").Return(cmd, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(11)).Return(nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOneWithProject(context.Background(), "noproj2", "42")

	require.NoError(t, err)
	require.Len(t, pusher.pushed, 1)
	assert.Equal(t, "42", pusher.pushed[0].ProjectID, "override project must be passed to pusher")
}

func TestSyncer_PushOneWithProject_empty_override_returns_error(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmd := &db.Command{ID: 12, ShortID: "noproj3", SyncedGW: false, ProjectID: ""}

	store.On("GetCommandByShortID", mock.Anything, "noproj3").Return(cmd, nil).Once()

	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)

	err := s.PushOneWithProject(context.Background(), "noproj3", "")

	require.Error(t, err)
	require.ErrorIs(t, err, grimsync.ErrNoProjectID)
	require.Empty(t, pusher.pushed)
}

func TestSyncBatch_skips_commands_without_project_id(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	cmds := []*db.Command{
		{ID: 1, ShortID: "noproj", ProjectID: "", SyncedGW: false},
		{ID: 2, ShortID: "withproj", ProjectID: "42", SyncedGW: false},
	}
	store.On("ListUnsynced", mock.Anything).Return(cmds, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(2)).Return(nil).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	require.Len(t, pusher.pushed, 1, "only the command with a project ID should be pushed")
	assert.Equal(t, int64(2), pusher.pushed[0].ID)
}
