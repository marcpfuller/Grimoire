// Package api implements the local REST endpoints that bridge the operator's
// shell hooks and the Grimoire daemon.
//
// Endpoints:
//
//	GET  /api/next-id    shell precmd reserves the short ID for the next command
//	POST /api/register   grim shell registers a new pane with the daemon
//	POST /api/execute    shell preexec records a command start
//	POST /api/precmd     shell precmd records a command end
//	POST /api/deregister shell exit removes the pane's pending reservation
//	POST /api/tag        grim tag attaches a label to a recorded command
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/util"
)

// Server holds the shared dependencies for all HTTP handlers.
type Server struct {
	store             db.Store
	maxShortIDRetries int
	logger            *slog.Logger
}

// New creates a Server. logger may be nil — all log calls become no-ops.
func New(store db.Store, maxShortIDRetries int, logger *slog.Logger) *Server {
	return &Server{
		store:             store,
		maxShortIDRetries: maxShortIDRetries,
		logger:            logger,
	}
}

// Handler returns an http.Handler with all API routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/next-id", s.handleNextID)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/execute", s.handleExecute)
	mux.HandleFunc("POST /api/precmd", s.handlePrecmd)
	mux.HandleFunc("POST /api/deregister", s.handleDeregister)
	mux.HandleFunc("POST /api/tag", s.handleTag)
	return mux
}

// --- request / response types ---

type nextIDResponse struct {
	ShortID string `json:"short_id"`
}

type registerRequest struct {
	PaneID    string `json:"pane_id"`
	ProjectID string `json:"project_id"`
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

type deregisterRequest struct {
	PaneID string `json:"pane_id"`
}

type tagRequest struct {
	ShortID string `json:"short_id"`
	Label   string `json:"label"`
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
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "pane not registered")
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

// handleRegister handles POST /api/register.
// Called by `grim shell` to register a pane with the daemon before spawning
// the shell process. Returns 204 on success.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PaneID == "" || req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "pane_id and project_id are required")
		return
	}
	if err := s.store.UpsertPane(r.Context(), req.PaneID, req.ProjectID, time.Now().UnixMilli()); err != nil {
		s.logWarn("register: UpsertPane failed", "pane_id", req.PaneID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to register pane")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleExecute handles POST /api/execute.
// It records a command start in the DB. The shell's preexec hook emits the
// enriched OSC activate marker into the PTY after this call returns, which
// triggers PTY capture in the local grim shell process.
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

	cmd := &db.Command{
		PaneID:    req.PaneID,
		Command:   req.Command,
		StartedAt: *req.StartedAt,
	}
	if err := s.store.CreateCommand(r.Context(), cmd); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// No pending reservation — shell must call GET /api/next-id first.
			writeError(w, http.StatusUnprocessableEntity, "no pending reservation for pane")
			return
		}
		s.logWarn("execute: CreateCommand failed", "pane_id", req.PaneID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create command")
		return
	}

	writeJSON(w, http.StatusOK, executeResponse{CommandID: cmd.ID, ShortID: cmd.ShortID})
}

// handlePrecmd handles POST /api/precmd.
// It records a command end in the DB. The shell's precmd hook emits the OSC
// deactivate marker into the PTY after this call returns, which stops PTY
// capture in the local grim shell process.
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

	if err := s.store.FinalizeCommand(r.Context(), req.ShortID, *req.EndedAt, req.ExitCode); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		s.logWarn("precmd: FinalizeCommand failed", "pane_id", req.PaneID, "short_id", req.ShortID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to finalize command")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTag handles POST /api/tag.
// It resolves the short ID to a command and attaches a label to it.
// Returns 409 Conflict if the label has already been applied to the command.
func (s *Server) handleTag(w http.ResponseWriter, r *http.Request) {
	var req tagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ShortID == "" || req.Label == "" {
		writeError(w, http.StatusBadRequest, "short_id and label are required")
		return
	}

	cmd, err := s.store.GetCommandByShortID(r.Context(), req.ShortID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		s.logWarn("tag: GetCommandByShortID failed", "short_id", req.ShortID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to look up command")
		return
	}

	if err = s.store.AddTag(r.Context(), cmd.ID, req.Label, time.Now().UnixMilli()); err != nil {
		if errors.Is(err, db.ErrDuplicateTag) {
			writeError(w, http.StatusConflict, "label already applied to this command")
			return
		}
		s.logWarn("tag: AddTag failed", "short_id", req.ShortID, "label", req.Label, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to add tag")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeregister handles POST /api/deregister.
// Called by grim shell on exit to clear the pending short ID reservation.
// The pane row and all its commands are retained for historical purposes.
func (s *Server) handleDeregister(w http.ResponseWriter, r *http.Request) {
	var req deregisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.PaneID == "" {
		writeError(w, http.StatusBadRequest, "pane_id is required")
		return
	}

	if err := s.store.DeregisterPane(r.Context(), req.PaneID); err != nil {
		s.logWarn("deregister: DeregisterPane failed", "pane_id", req.PaneID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to deregister pane")
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
