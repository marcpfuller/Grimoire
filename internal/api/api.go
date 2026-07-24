// Package api implements the local REST endpoints that bridge the operator's
// shell hooks and the Grimoire daemon.
//
// Endpoints:
//
//	GET  /api/next-id   shell precmd reserves the short ID for the next command
//	POST /api/execute   shell preexec records a command start and activates PTY capture
//	POST /api/precmd    shell precmd records a command end and deactivates PTY capture
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/util"
)

// PtySession is the subset of pty.Session consumed by the API handlers.
// Defined as an interface so the api package does not import internal/pty,
// and so handlers can be tested without a real PTY.
type PtySession interface {
	Activate(commandID, startedAt int64, shortID string)
	Deactivate()
}

// SessionRegistry provides read-only access to live PTY sessions by pane ID.
// Implemented by the daemon's session manager in main.go using a
// sync.RWMutex-protected map.
type SessionRegistry interface {
	Lookup(paneID string) (PtySession, bool)
}

// Server holds the shared dependencies for all HTTP handlers.
type Server struct {
	store             db.Store
	sessions          SessionRegistry
	maxShortIDRetries int
	logger            *slog.Logger
}

// New creates a Server. logger may be nil — all log calls become no-ops.
func New(store db.Store, sessions SessionRegistry, maxShortIDRetries int, logger *slog.Logger) *Server {
	return &Server{
		store:             store,
		sessions:          sessions,
		maxShortIDRetries: maxShortIDRetries,
		logger:            logger,
	}
}

// Handler returns an http.Handler with all API routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/next-id", s.handleNextID)
	mux.HandleFunc("POST /api/execute", s.handleExecute)
	mux.HandleFunc("POST /api/precmd", s.handlePrecmd)
	return mux
}

// --- request / response types ---

type nextIDResponse struct {
	ShortID string `json:"short_id"`
}

type executeRequest struct {
	PaneID    string `json:"pane_id"`
	Command   string `json:"command"`
	StartedAt *int64 `json:"started_at"`
}

type executeResponse struct {
	CommandID int64  `json:"command_id"`
	ShortID   string `json:"short_id"`
}

type precmdRequest struct {
	PaneID   string `json:"pane_id"`
	ShortID  string `json:"short_id"`
	EndedAt  *int64 `json:"ended_at"`
	ExitCode int    `json:"exit_code"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// --- handlers ---

// handleNextID handles GET /api/next-id?pane=<id>.
// It generates and reserves a short ID for the next command on the pane.
func (s *Server) handleNextID(w http.ResponseWriter, r *http.Request) {
	paneID := r.URL.Query().Get("pane")
	if paneID == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: pane")
		return
	}

	// Fail fast — if no live session exists for this pane the daemon cannot
	// route output to any command bucket.
	if _, ok := s.sessions.Lookup(paneID); !ok {
		writeError(w, http.StatusServiceUnavailable, "no active session for pane")
		return
	}

	// Retry loop: generate a random short ID and attempt to reserve it.
	// ErrConflict means the generated ID is already in use — extremely rare
	// (28-bit space) but bounded by s.maxShortIDRetries.
	for i := range s.maxShortIDRetries {
		shortID, err := util.GenerateShortID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate short ID")
			return
		}

		err = s.store.ReserveShortID(r.Context(), paneID, shortID)
		if err == nil {
			writeJSON(w, http.StatusOK, nextIDResponse{ShortID: shortID})
			return
		}
		if !errors.Is(err, db.ErrConflict) {
			s.logWarn("next-id: reserve failed", "pane_id", paneID, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to reserve short ID")
			return
		}
		s.logWarn("next-id: short_id conflict, retrying", "pane_id", paneID, "attempt", i+1)
	}

	s.logWarn("next-id: exhausted retries", "pane_id", paneID, "max_retries", s.maxShortIDRetries)
	writeError(w, http.StatusInternalServerError,
		fmt.Sprintf("short ID conflict after %d retries", s.maxShortIDRetries))
}

// handleExecute handles POST /api/execute.
// It records a command start in the DB and activates PTY capture.
func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PaneID == "" || req.Command == "" || req.StartedAt == nil {
		writeError(w, http.StatusBadRequest, "pane_id, command, and started_at are required")
		return
	}

	sess, ok := s.sessions.Lookup(req.PaneID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "no active session for pane")
		return
	}

	cmd := &db.Command{
		PaneID:    req.PaneID,
		Command:   req.Command,
		StartedAt: *req.StartedAt,
	}
	if err := s.store.CreateCommand(r.Context(), cmd); err != nil {
		s.logWarn("execute: CreateCommand failed", "pane_id", req.PaneID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create command")
		return
	}

	sess.Activate(cmd.ID, cmd.StartedAt, cmd.ShortID)
	writeJSON(w, http.StatusOK, executeResponse{CommandID: cmd.ID, ShortID: cmd.ShortID})
}

// handlePrecmd handles POST /api/precmd.
// It records a command end in the DB. Deactivate is always called via defer
// so the PTY capture state is never left stuck, even on DB failure.
func (s *Server) handlePrecmd(w http.ResponseWriter, r *http.Request) {
	var req precmdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PaneID == "" || req.ShortID == "" || req.EndedAt == nil {
		writeError(w, http.StatusBadRequest, "pane_id, short_id, and ended_at are required")
		return
	}

	sess, ok := s.sessions.Lookup(req.PaneID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "no active session for pane")
		return
	}

	// Deactivate always fires — a DB failure must not leave the PTY stuck in
	// Capturing state for the lifetime of the session.
	defer sess.Deactivate()

	if err := s.store.FinalizeCommand(r.Context(), req.ShortID, *req.EndedAt, req.ExitCode); err != nil {
		s.logWarn("precmd: FinalizeCommand failed", "short_id", req.ShortID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to finalize command")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func (s *Server) logWarn(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(msg, args...)
	}
}
