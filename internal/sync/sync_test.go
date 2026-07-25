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
	return grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)
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

	assert.Equal(t, cmds, pusher.pushed, "both commands must be pushed")
}

func TestSyncer_RunOnce_empty_batch(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	store.On("ListUnsynced", mock.Anything).Return([]*db.Command{}, nil).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	assert.Empty(t, pusher.pushed, "nothing to push")
}

func TestSyncer_RunOnce_list_failure(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockPusher{}
	store.On("ListUnsynced", mock.Anything).Return(([]*db.Command)(nil), errors.New("db down")).Once()

	newSyncer(t, store, pusher).RunOnce(context.Background())

	assert.Empty(t, pusher.pushed, "no push when ListUnsynced fails")
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
	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Hour}, nil)

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
	s := grimsync.New(store, pusher, grimsync.Config{Interval: 5 * time.Millisecond}, nil)
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
	s := grimsync.New(store, pusher, grimsync.Config{Interval: time.Hour}, nil)

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
	assert.ErrorIs(t, ae.Unwrap(), inner)
}
