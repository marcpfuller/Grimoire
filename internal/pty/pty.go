package pty

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/marcpfuller/grimoire/internal/db"
)

// oscMarker is the shell integration sequence emitted by the preexec hook
// immediately before a command runs. It signals the PTY read loop to begin
// routing output to the active command bucket.
//
// Format: ESC ] 133 ; C ST  (ST = ESC \)
// Compatible with the iTerm2 / Warp shell integration protocol.
// The marker is stripped from the passthrough stream so the user never sees it.
var oscMarker = []byte("\x1b]133;C\x1b\\")

// captureState is the shared handshake point between the HTTP API handlers
// (which know the command metadata) and the PTY read loop (which sees the
// raw byte stream). The mutex ensures these two goroutines never race.
type captureState struct {
	mu        sync.Mutex
	commandID int64 // 0 means Idle; non-zero means Capturing
	startedAt int64 // Unix ms, set alongside commandID
}

func (s *captureState) activate(commandID, startedAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commandID = commandID
	s.startedAt = startedAt
}

func (s *captureState) deactivate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commandID = 0
	s.startedAt = 0
}

func (s *captureState) snapshot() (commandID, startedAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commandID, s.startedAt
}

// Session wraps a single `grimoire shell` invocation. One Session is created
// per `grimoire shell` call; it owns the PTY master and the child shell process.
type Session struct {
	paneID string
	shell  string
	store  db.Store
	state  captureState
	// out receives every byte that flows through the PTY (both passthrough and
	// captured). Defaults to os.Stdout; injectable for tests.
	out io.Writer
}

// New returns a Session that will spawn shell under a PTY and register paneID
// in the store. Call Run to start the session.
func New(store db.Store, paneID, shell string) *Session {
	return &Session{
		paneID: paneID,
		shell:  shell,
		store:  store,
		out:    os.Stdout,
	}
}

// PaneID returns the GRIMOIRE_PANE_ID for this session.
func (s *Session) PaneID() string { return s.paneID }

// Activate is called by the HTTP handler for POST /api/execute once
// CreateCommand has succeeded. It switches the read loop from Idle to
// Capturing for this session.
//
// Why no race: the shell is blocked on the HTTP call (curl in preexec) so no
// command output can appear in the PTY until this method returns and the HTTP
// response is sent back to the shell.
func (s *Session) Activate(commandID, startedAt int64) {
	s.state.activate(commandID, startedAt)
}

// Deactivate is called by the HTTP handler for POST /api/precmd once
// FinalizeCommand has succeeded. It switches the read loop back to Idle.
func (s *Session) Deactivate() {
	s.state.deactivate()
}

// Run spawns the child shell, wires I/O, and blocks until the shell exits.
// On return the caller should exit or return to its own shell (ssh semantics).
func (s *Session) Run(ctx context.Context) error {
	// Spawn the shell without --login so it sources the interactive rc file
	// (.zshrc / .bashrc) rather than the login profile. creack/pty attaches a
	// real TTY so the shell enters interactive mode automatically.
	cmd := exec.CommandContext(ctx, s.shell)
	cmd.Env = append(os.Environ(), "GRIMOIRE_PANE_ID="+s.paneID)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty: start %s: %w", s.shell, err)
	}
	defer ptmx.Close()

	// Forward terminal resize signals to the child PTY.
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	defer func() {
		signal.Stop(resizeCh)
		close(resizeCh)
	}()
	go func() {
		for range resizeCh {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	resizeCh <- syscall.SIGWINCH // set initial size

	// Put the controlling terminal into raw mode so all keystrokes are
	// forwarded verbatim to the child PTY without local echo or line editing.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("pty: raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState) //nolint:errcheck

	// stdin → child PTY (user keystrokes).
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// child PTY → stdout + DB (command output capture).
	s.readLoop(ctx, ptmx)

	return cmd.Wait()
}

// readLoop reads from the PTY master until EOF (shell exit). Each read is
// routed based on the current captureState:
//
//   - Idle (commandID == 0): bytes are written to out only.
//   - Capturing (commandID != 0): bytes are written to out AND stored as
//     an output_chunk row in the database.
//
// The OSC marker sequence is stripped from the passthrough stream whenever it
// is detected, regardless of capture state.
func (s *Session) readLoop(ctx context.Context, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			// Single snapshot per read: routing and storage use the same
			// commandID, eliminating a TOCTOU race between the two decisions.
			cmdID, startedAt := s.state.snapshot()
			chunk := buf[:n]
			passthrough, captured := s.splitOnMarker(chunk, cmdID)

			if len(passthrough) > 0 {
				_, _ = s.out.Write(passthrough)
				// When Capturing, bytes before a mid-buffer marker are the
				// tail of the current command's output; store them too.
				if cmdID != 0 {
					s.storeChunk(ctx, passthrough, cmdID, startedAt)
				}
			}
			if len(captured) > 0 {
				_, _ = s.out.Write(captured)
				if cmdID != 0 {
					s.storeChunk(ctx, captured, cmdID, startedAt)
				}
			}
		}
		if err != nil {
			// EOF is normal when the child shell exits.
			return
		}
	}
}

// splitOnMarker splits a PTY read buffer around the OSC marker.
// It returns (passthrough, captured):
//   - passthrough: bytes before the marker (written to out, not stored)
//   - captured: bytes after the marker (written to out AND stored)
//
// If no marker is found, the bytes are routed based on the current
// captureState: Idle → (buf, nil); Capturing → (nil, buf).
//
// NOTE: only the first marker in a buffer is stripped. A second marker in
// the same read would pass through as raw escape bytes. This cannot occur
// in normal flow (precmd always runs between commands, resetting state before
// the next preexec emits a new marker) but is documented here for clarity.
//
// cmdID must be the value already snapshotted by the caller; splitOnMarker
// never reads captureState itself, avoiding a redundant lock acquisition.
func (s *Session) splitOnMarker(buf []byte, cmdID int64) (passthrough, captured []byte) {
	if idx := bytes.Index(buf, oscMarker); idx >= 0 {
		// Bytes before the marker belong to Idle state (the prompt / line editor).
		// Bytes after the marker belong to Capturing state.
		return buf[:idx], buf[idx+len(oscMarker):]
	}

	if cmdID != 0 {
		return nil, buf
	}
	return buf, nil
}

// storeChunk persists buf as an output_chunk under cmdID. The caller is
// responsible for ensuring cmdID != 0. Store failure is non-fatal: the
// operator still sees output via out; errors will surface via the daemon
// logger once one is wired.
func (s *Session) storeChunk(ctx context.Context, buf []byte, cmdID, startedAt int64) {
	nowMs := time.Now().UnixMilli()
	offsetMs := nowMs - startedAt
	if offsetMs < 0 {
		offsetMs = 0
	}

	data := make([]byte, len(buf))
	copy(data, buf)

	chunk := &db.OutputChunk{
		CommandID: cmdID,
		OffsetMs:  offsetMs,
		Data:      data,
	}
	_ = s.store.AppendOutputChunk(ctx, chunk)
}
