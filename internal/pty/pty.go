package pty

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/marcpfuller/grimoire/internal/db"
)

// activatePrefix is the fixed start of the enriched activate OSC sequence.
// Full format: ESC ] 133 ; C ; id=N ; start=N ; sid=S ST  (ST = ESC \)
// The shell's preexec hook emits this after POST /api/execute returns the
// command_id and short_id. The PTY read loop parses the embedded metadata
// to self-activate without any in-process callback from the HTTP server.
var activatePrefix = []byte("\x1b]133;C;")

// oscST is the OSC string terminator (ESC \).
var oscST = []byte("\x1b\\")

// deactivateMarker is the full deactivate OSC sequence.
// The shell's precmd hook emits this AFTER POST /api/precmd returns 204 so
// that FinalizeCommand always completes before the PTY stops capturing.
var deactivateMarker = []byte("\x1b]133;D\x1b\\")

// activateParams holds the parsed fields from an enriched activate marker.
type activateParams struct {
	commandID int64
	startedAt int64
	shortID   string
}

// buildActivateMarker constructs the enriched OSC activate sequence for a
// command. Shell integration scripts emit this after POST /api/execute returns.
func buildActivateMarker(commandID, startedAt int64, shortID string) []byte {
	return []byte(fmt.Sprintf("\x1b]133;C;id=%d;start=%d;sid=%s\x1b\\",
		commandID, startedAt, shortID))
}

// findActivateMarker searches buf for the enriched activate OSC marker.
// Returns:
//   - before: byte index where the marker starts (bytes before are passthrough)
//   - after: byte index where the marker ends (bytes from here are captured)
//   - params: the parsed activate parameters
//   - found: true if a complete, valid marker was found
//
// Returns found=false if the marker is absent, incomplete (spans a buffer
// boundary), or has unparseable parameters.
func findActivateMarker(buf []byte) (before, after int, params activateParams, found bool) {
	idx := bytes.Index(buf, activatePrefix)
	if idx < 0 {
		return 0, 0, activateParams{}, false
	}
	rest := buf[idx+len(activatePrefix):]
	stIdx := bytes.Index(rest, oscST)
	if stIdx < 0 {
		// Marker spans a buffer boundary — skip this read.
		return 0, 0, activateParams{}, false
	}
	p, err := parseActivateParams(rest[:stIdx])
	if err != nil {
		return 0, 0, activateParams{}, false
	}
	return idx, idx + len(activatePrefix) + stIdx + len(oscST), p, true
}

// parseActivateParams parses the parameter string between the OSC prefix and
// the string terminator, e.g. "id=42;start=1234567890000;sid=abc1234".
func parseActivateParams(paramStr []byte) (activateParams, error) {
	var p activateParams
	for _, kv := range bytes.Split(paramStr, []byte(";")) {
		eq := bytes.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := string(kv[:eq])
		val := string(kv[eq+1:])
		switch key {
		case "id":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return activateParams{}, fmt.Errorf("invalid id: %w", err)
			}
			p.commandID = n
		case "start":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return activateParams{}, fmt.Errorf("invalid start: %w", err)
			}
			p.startedAt = n
		case "sid":
			p.shortID = val
		}
	}
	if p.commandID == 0 || p.startedAt == 0 || p.shortID == "" {
		return activateParams{}, fmt.Errorf("missing required params (id, start, sid)")
	}
	return p, nil
}

// captureState is the shared handshake point between the HTTP API handlers
// (which know the command metadata) and the PTY read loop (which sees the
// raw byte stream). snapshot is called on every PTY read, so it uses RLock
// to allow concurrent reads without contention.
type captureState struct {
	mu        sync.RWMutex
	commandID int64  // 0 means Idle; non-zero means Capturing
	startedAt int64  // Unix ms, set alongside commandID
	shortID   string // 7-hex short identifier for the active command
}

func (s *captureState) activate(commandID, startedAt int64, shortID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commandID = commandID
	s.startedAt = startedAt
	s.shortID = shortID
}

func (s *captureState) deactivate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commandID = 0
	s.startedAt = 0
	s.shortID = ""
}

func (s *captureState) snapshot() (commandID, startedAt int64, shortID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.commandID, s.startedAt, s.shortID
}

// Session wraps a single `grimoire shell` invocation. One Session is created
// per `grimoire shell` call; it owns the PTY master and the child shell process.
type Session struct {
	cfg   SessionConfig
	store db.Store
	state captureState
	// out receives every byte that flows through the PTY (both passthrough and
	// captured). Defaults to os.Stdout; injectable for tests.
	out io.Writer
	// logger is pre-bound with pane_id. nil is safe — log calls are no-ops.
	logger *slog.Logger
}

// SessionConfig holds all configuration for a Session.
type SessionConfig struct {
	// PaneID is exported to the child shell as GRIMOIRE_PANE_ID.
	PaneID string
	// Shell is the path to the shell binary to spawn.
	Shell string
	// DaemonAddr is exported to the child shell as GRIMOIRE_DAEMON_ADDR.
	DaemonAddr string
	// ShellArgs are appended to the shell command (e.g. ["--init-file", "/tmp/rc"]).
	ShellArgs []string
	// ExtraEnv are additional KEY=VALUE pairs injected into the child env
	// (e.g. ["ZDOTDIR=/tmp/grimoire-zdotdir-xyz"]).
	ExtraEnv []string
}

// New returns a Session that will spawn the configured shell under a PTY.
// Call Run to start the session.
//
// logger should be pre-bound with daemon-level attributes via slog.With; New
// further binds pane_id so every log line carries it automatically. Pass nil
// to suppress logging (useful in tests).
func New(store db.Store, cfg SessionConfig, logger *slog.Logger) *Session {
	var log *slog.Logger
	if logger != nil {
		log = logger.With("pane_id", cfg.PaneID)
	}
	return &Session{
		cfg:    cfg,
		store:  store,
		out:    os.Stdout,
		logger: log,
	}
}

// PaneID returns the GRIMOIRE_PANE_ID for this session.
func (s *Session) PaneID() string { return s.cfg.PaneID }

// Run spawns the child shell, wires I/O, and blocks until the shell exits.
// On return the caller should exit or return to its own shell (ssh semantics).
func (s *Session) Run(ctx context.Context) error {
	// Spawn the shell without --login so it sources the interactive rc file
	// (.zshrc / .bashrc) rather than the login profile. creack/pty attaches a
	// real TTY so the shell enters interactive mode automatically.
	// s.cfg.Shell is set by the operator via config, not from network input.
	cmd := exec.CommandContext(ctx, s.cfg.Shell, s.cfg.ShellArgs...) //nolint:gosec
	cmd.Env = append(os.Environ(),
		"GRIMOIRE_PANE_ID="+s.cfg.PaneID,
		"GRIMOIRE_DAEMON_ADDR="+s.cfg.DaemonAddr,
	)
	cmd.Env = append(cmd.Env, s.cfg.ExtraEnv...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty: start %s: %w", s.cfg.Shell, err)
	}
	defer ptmx.Close() //nolint:errcheck // PTY close errors are unactionable after shell exit

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

	// child PTY → out + DB (command output capture).
	s.readLoop(ctx, ptmx)

	return cmd.Wait()
}

// readLoop reads from the PTY master until EOF (shell exit). Each read is
// dispatched to processChunk, which handles OSC marker detection and routing.
//
// Known limitation: if an activate OSC sequence is split across two consecutive
// reads (activatePrefix in buf N, ST terminator in buf N+1), findActivateMarker
// returns found=false and the partial prefix bytes are written to out as raw
// characters. A carry buffer across reads would fix this but adds complexity;
// with a 4096-byte buffer and the short OSC sequences emitted by shell hooks
// this split is extremely unlikely in practice.
func (s *Session) readLoop(ctx context.Context, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.processChunk(ctx, buf[:n])
		}
		if err != nil {
			// EOF is normal when the child shell exits.
			return
		}
	}
}

// processChunk routes a single PTY read buffer based on OSC marker content
// and current capture state:
//
//   - Deactivate marker (\x1b]133;D\x1b\): bytes before are stored if capturing,
//     then captureState is cleared. promptBuf is reset. Bytes after continue
//     processing (they may contain the A marker in the same read).
//   - Prompt-start marker (\x1b]133;A\x1b\): resets promptBuf (discards bytes
//     accumulated since D). Bytes after are written to out and accumulated into
//     the fresh promptBuf — these are the PS1 text. Bytes after continue
//     processing (they may contain an activate C marker in the same read).
//   - Activate marker (\x1b]133;C;id=N;...\x1b\): bytes before are passthrough
//     (or stored if unexpectedly still capturing), captureState is set from the
//     embedded params, bytes after are stored under the new commandID.
//   - No marker: bytes are written to out; if capturing, also stored in DB;
//     if idle and promptCapturing, accumulated in promptBuf.
// processChunk routes a single PTY read buffer based on OSC marker content
// and current capture state:
//
//   - Deactivate marker (\x1b]133;D\x1b\): bytes before are stored if capturing,
//     then captureState is cleared. Bytes after continue processing (same buffer).
//   - Activate marker (\x1b]133;C;id=N;...\x1b\): prompt+command context is
//     provided by the shell hook via POST /api/execute, so any bytes before the
//     marker are written to out but not stored. captureState is set from the
//     embedded params, bytes after are stored under the new commandID.
//   - No marker: bytes are written to out; if capturing, also stored in DB.
//
// The inner loop ensures that a D followed by C in the same PTY read (e.g. back-
// to-back commands with no idle gap) is handled correctly.
func (s *Session) processChunk(ctx context.Context, chunk []byte) {
	for len(chunk) > 0 {
		// --- Deactivate marker ---
		if dIdx := bytes.Index(chunk, deactivateMarker); dIdx >= 0 {
			before := chunk[:dIdx]
			after := chunk[dIdx+len(deactivateMarker):]
			cmdID, startedAt, shortID := s.state.snapshot()
			if len(before) > 0 {
				_, _ = s.out.Write(before)
				if cmdID != 0 {
					s.storeChunk(ctx, before, cmdID, startedAt, shortID)
				}
			}
			s.state.deactivate()
			chunk = after
			continue
		}

		// --- Activate marker ---
		if bIdx, aIdx, params, found := findActivateMarker(chunk); found {
			before := chunk[:bIdx]
			after := chunk[aIdx:]
			cmdID, startedAt, shortID := s.state.snapshot()
			if len(before) > 0 {
				_, _ = s.out.Write(before)
				if cmdID != 0 {
					// Edge case: still capturing a previous command. Store the
					// tail under the outgoing ID.
					s.storeChunk(ctx, before, cmdID, startedAt, shortID)
				}
				// Idle-state before bytes are PS1/typed text — the hook already
				// sent the clean prompt string via POST /api/execute, so we skip
				// storing these PTY bytes to avoid duplication.
			}
			s.state.activate(params.commandID, params.startedAt, params.shortID)
			if len(after) > 0 {
				_, _ = s.out.Write(after)
				s.storeChunk(ctx, after, params.commandID, params.startedAt, params.shortID)
			}
			return
		}

		// --- Normal routing ---
		cmdID, startedAt, shortID := s.state.snapshot()
		_, _ = s.out.Write(chunk)
		if cmdID != 0 {
			s.storeChunk(ctx, chunk, cmdID, startedAt, shortID)
		}
		return
	}
}

// storeChunk persists buf as an output_chunk under cmdID. The caller is
// responsible for ensuring cmdID != 0. Store failure is logged at WARN
// (non-fatal: the operator still sees output via out).
func (s *Session) storeChunk(ctx context.Context, buf []byte, cmdID, startedAt int64, shortID string) {
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
	if err := s.store.AppendOutputChunk(ctx, chunk); err != nil && s.logger != nil {
		s.logger.Warn("output chunk dropped",
			"short_id", shortID,
			"cmd_id", cmdID,
			"err", err,
		)
	}
}
