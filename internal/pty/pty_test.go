package pty

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/db"
)

// --- captureState ---

func TestCaptureState(t *testing.T) {
	t.Run("idle_by_default", func(t *testing.T) {
		var s captureState
		cmdID, startedAt := s.snapshot()
		assert.Zero(t, cmdID)
		assert.Zero(t, startedAt)
	})

	t.Run("activate_and_snapshot", func(t *testing.T) {
		var s captureState
		s.activate(42, 1000)
		cmdID, startedAt := s.snapshot()
		assert.Equal(t, int64(42), cmdID)
		assert.Equal(t, int64(1000), startedAt)
	})

	t.Run("deactivate_clears_state", func(t *testing.T) {
		var s captureState
		s.activate(42, 1000)
		s.deactivate()
		cmdID, startedAt := s.snapshot()
		assert.Zero(t, cmdID)
		assert.Zero(t, startedAt)
	})

	t.Run("concurrent_activate_and_snapshot", func(t *testing.T) {
		// Exercises the RWMutex under -race: multiple snapshot readers run
		// concurrently while a writer calls activate, then deactivate.
		var s captureState
		const readers = 50

		ready := make(chan struct{})
		done := make(chan struct{}, readers)

		for i := range readers {
			go func(i int) {
				<-ready
				_, _ = s.snapshot()
				done <- struct{}{}
			}(i)
		}

		close(ready) // release all readers simultaneously
		s.activate(99, 1000)
		s.deactivate()

		for range readers {
			<-done
		}
		cmdID, _ := s.snapshot()
		assert.Zero(t, cmdID)
	})
}

// --- splitOnMarker ---

func TestSplitOnMarker(t *testing.T) {
	marker := string(oscMarker)

	t.Run("marker_at_start_idle_before", func(t *testing.T) {
		input := []byte(marker + "output data")
		pass, cap := splitOnMarker(input, 0)
		assert.Empty(t, pass)
		assert.Equal(t, []byte("output data"), cap)
	})

	t.Run("marker_in_middle_with_idle_prefix", func(t *testing.T) {
		input := []byte("prompt text" + marker + "cmd output")
		pass, cap := splitOnMarker(input, 0)
		assert.Equal(t, []byte("prompt text"), pass)
		assert.Equal(t, []byte("cmd output"), cap)
	})

	t.Run("marker_at_end_nothing_after", func(t *testing.T) {
		input := []byte("prompt" + marker)
		pass, cap := splitOnMarker(input, 0)
		assert.Equal(t, []byte("prompt"), pass)
		assert.Empty(t, cap)
	})

	t.Run("no_marker_idle_state_all_passthrough", func(t *testing.T) {
		input := []byte("just prompt output")
		pass, cap := splitOnMarker(input, 0)
		assert.Equal(t, input, pass)
		assert.Nil(t, cap)
	})

	t.Run("no_marker_capturing_state_all_stored", func(t *testing.T) {
		input := []byte("command stdout bytes")
		pass, cap := splitOnMarker(input, 7)
		assert.Nil(t, pass)
		assert.Equal(t, input, cap)
	})

	t.Run("marker_resets_to_capturing_mid_buffer", func(t *testing.T) {
		// When a marker is found, bytes before it always go to passthrough and
		// bytes after go to captured — regardless of the current capture state.
		// In normal flow this case cannot occur: precmd always calls Deactivate
		// before the next preexec emits a new marker, so the session is Idle by
		// the time any marker arrives. Documented here for completeness.
		input := []byte("tail of cmd1" + marker + "start of cmd2")
		pass, cap := splitOnMarker(input, 7)
		assert.Equal(t, []byte("tail of cmd1"), pass)
		assert.Equal(t, []byte("start of cmd2"), cap)
	})
}

// --- maybeStore ---

// mockStore captures AppendOutputChunk calls for inspection.
type mockStore struct {
	db.Store
	chunks []*db.OutputChunk
}

func (m *mockStore) AppendOutputChunk(_ context.Context, chunk *db.OutputChunk) error {
	c := &db.OutputChunk{
		CommandID: chunk.CommandID,
		OffsetMs:  chunk.OffsetMs,
		Data:      append([]byte(nil), chunk.Data...),
	}
	m.chunks = append(m.chunks, c)
	return nil
}

func TestStoreChunk(t *testing.T) {
	t.Run("stores_chunk", func(t *testing.T) {
		ms := &mockStore{}
		startedAt := time.Now().UnixMilli()
		sess := &Session{store: ms}

		sess.storeChunk(context.Background(), []byte("hello"), 99, startedAt)

		require.Len(t, ms.chunks, 1)
		assert.Equal(t, int64(99), ms.chunks[0].CommandID)
		assert.Equal(t, []byte("hello"), ms.chunks[0].Data)
		assert.GreaterOrEqual(t, ms.chunks[0].OffsetMs, int64(0))
	})

	t.Run("offset_ms_non_negative_even_if_clock_skew", func(t *testing.T) {
		ms := &mockStore{}
		futureStart := time.Now().UnixMilli() + 9999 // startedAt in the future
		sess := &Session{store: ms}

		sess.storeChunk(context.Background(), []byte("data"), 1, futureStart)

		require.Len(t, ms.chunks, 1)
		assert.Zero(t, ms.chunks[0].OffsetMs, "offset_ms must not be negative")
	})

	t.Run("data_is_copied_not_aliased", func(t *testing.T) {
		ms := &mockStore{}
		sess := &Session{store: ms}
		startedAt := time.Now().UnixMilli()

		buf := []byte("original")
		sess.storeChunk(context.Background(), buf, 1, startedAt)
		buf[0] = 'X' // mutate original

		require.Len(t, ms.chunks, 1)
		assert.Equal(t, []byte("original"), ms.chunks[0].Data, "stored data must be a copy")
	})
}

// --- readLoop integration ---

func TestReadLoop(t *testing.T) {
	t.Run("passthrough_only_in_idle_state", func(t *testing.T) {
		ms := &mockStore{}
		var out bytes.Buffer
		sess := &Session{store: ms, out: &out}

		sess.readLoop(context.Background(), bytes.NewReader([]byte("prompt text")))

		assert.Equal(t, "prompt text", out.String(), "idle bytes must reach out")
		assert.Empty(t, ms.chunks, "idle state: nothing stored")
	})

	t.Run("capturing_state_writes_to_out_and_store", func(t *testing.T) {
		ms := &mockStore{}
		var out bytes.Buffer
		sess := &Session{store: ms, out: &out}
		sess.state.activate(5, time.Now().UnixMilli())

		sess.readLoop(context.Background(), bytes.NewReader([]byte("command output bytes")))

		assert.Equal(t, "command output bytes", out.String(), "captured bytes must still reach out")
		require.NotEmpty(t, ms.chunks)
		var stored []byte
		for _, c := range ms.chunks {
			stored = append(stored, c.Data...)
		}
		assert.Equal(t, []byte("command output bytes"), stored)
	})

	t.Run("marker_stripped_from_out", func(t *testing.T) {
		ms := &mockStore{}
		var out bytes.Buffer
		sess := &Session{store: ms, out: &out}

		payload := append(append([]byte("prompt"), oscMarker...), []byte("ls output")...)
		sess.readLoop(context.Background(), bytes.NewReader(payload))

		// OSC marker bytes must not appear in out.
		assert.Equal(t, "promptls output", out.String(), "marker must be stripped")
		assert.Empty(t, ms.chunks, "commandID is 0; nothing stored")
	})

	t.Run("capturing_with_mid_buffer_marker_stores_passthrough", func(t *testing.T) {
		// When Capturing and a marker appears mid-buffer, the bytes before the
		// marker are the tail of the current command's output and must be stored.
		// The bytes after the marker begin the next command; they are also stored
		// under the current commandID (best-effort: this edge case cannot occur
		// in normal flow where precmd always fires before the next preexec).
		ms := &mockStore{}
		var out bytes.Buffer
		sess := &Session{store: ms, out: &out}
		sess.state.activate(5, time.Now().UnixMilli())

		payload := append(append([]byte("tail of cmd"), oscMarker...), []byte("start of next")...)
		sess.readLoop(context.Background(), bytes.NewReader(payload))

		// Both halves must reach out (marker stripped).
		assert.Equal(t, "tail of cmdstart of next", out.String())
		// Both halves must be stored.
		require.NotEmpty(t, ms.chunks)
		var stored []byte
		for _, c := range ms.chunks {
			stored = append(stored, c.Data...)
		}
		assert.Equal(t, []byte("tail of cmdstart of next"), stored)
	})
}
