// Package api implements the local REST endpoints that bridge the operator's
// shell hooks and the Grimoire daemon.
//
// Endpoints:
//
//	 GET  /api/health                    daemon liveness probe used by grim shell watcher
//	GET  /api/next-id                   shell precmd reserves the short ID for the next command
//	POST /api/register                  grim shell registers a new pane with the daemon
//	POST /api/execute                   shell preexec records a command start
//	POST /api/precmd                    shell precmd records a command end
//	POST /api/deregister                shell exit removes the pane's pending reservation
//	POST /api/tag                       grim tag attaches a label to a recorded command
//	GET  /api/commands/{id}/metadata    fetch operator annotations for a command
//	PATCH /api/commands/{id}/metadata   update operator annotations for a command
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/cast"
	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/scrub"
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
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/sessions", s.handleGetSessions)
	mux.HandleFunc("GET /api/tags", s.handleGetTags)
	mux.HandleFunc("GET /api/next-id", s.handleNextID)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/execute", s.handleExecute)
	mux.HandleFunc("POST /api/precmd", s.handlePrecmd)
	mux.HandleFunc("POST /api/deregister", s.handleDeregister)
	mux.HandleFunc("POST /api/tag", s.handleTag)
	mux.HandleFunc("GET /api/commands/{id}/output", s.handleGetOutput)
	mux.HandleFunc("GET /api/commands/{id}/metadata", s.handleGetMetadata)
	mux.HandleFunc("PATCH /api/commands/{id}/metadata", s.handlePatchMetadata)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handleUpdateConfig)
	mux.HandleFunc("POST /api/cast", s.handleExportCast)
	return mux
}

// --- request / response types ---

type nextIDResponse struct {
	ShortID string `json:"short_id"`
}

type registerRequest struct {
	PaneID    string `json:"pane_id"`
	ProjectID string `json:"project_id"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
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

type castRequest struct {
	ID string `json:"id"`
}

type castExportResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
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
	if req.Width <= 0 {
		req.Width = 220
	}
	if req.Height <= 0 {
		req.Height = 50
	}
	if err := s.store.UpsertPane(r.Context(), req.PaneID, req.ProjectID, time.Now().UnixMilli(), req.Width, req.Height); err != nil {
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

func (s *Server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	shortID := r.PathValue("id")
	if shortID == "" {
		writeError(w, http.StatusBadRequest, "missing command id")
		return
	}

	cmd, err := s.store.GetCommandByShortID(r.Context(), shortID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "command not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up command")
		return
	}

	meta, err := s.store.GetCommandMetadata(r.Context(), cmd.ID)
	if errors.Is(err, db.ErrNotFound) {
		meta = &db.CommandMetadata{
			CommandID: cmd.ID,
			Tool:      toolFromCommand(cmd.Command),
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load metadata")
		return
	}

	tags, err := s.store.GetTagsByCommand(r.Context(), cmd.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tags")
		return
	}
	tagLabels := make([]string, len(tags))
	for i, t := range tags {
		tagLabels[i] = t.Label
	}

	writeJSON(w, http.StatusOK, MetadataResponse{
		Operator:      meta.Operator,
		Tool:          meta.Tool,
		UserContext:   meta.UserContext,
		SourceIP:      meta.SourceIP,
		DestinationIP: meta.DestinationIP,
		Description:   meta.Description,
		Comments:      meta.Comments,
		Tags:          tagLabels,
	})
}

func (s *Server) handlePatchMetadata(w http.ResponseWriter, r *http.Request) {
	shortID := r.PathValue("id")
	if shortID == "" {
		writeError(w, http.StatusBadRequest, "missing command id")
		return
	}

	var req MetadataUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	cmd, err := s.store.GetCommandByShortID(r.Context(), shortID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "command not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up command")
		return
	}

	meta, err := s.store.GetCommandMetadata(r.Context(), cmd.ID)
	if errors.Is(err, db.ErrNotFound) {
		meta = &db.CommandMetadata{
			CommandID: cmd.ID,
			Tool:      toolFromCommand(cmd.Command),
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load metadata")
		return
	}

	if req.Operator != nil {
		meta.Operator = *req.Operator
	}
	if req.Tool != nil {
		meta.Tool = *req.Tool
	}
	if req.UserContext != nil {
		meta.UserContext = *req.UserContext
	}
	if req.SourceIP != nil {
		meta.SourceIP = *req.SourceIP
	}
	if req.DestinationIP != nil {
		meta.DestinationIP = *req.DestinationIP
	}
	if req.Description != nil {
		meta.Description = *req.Description
	}
	if req.Comments != nil {
		meta.Comments = *req.Comments
	}
	meta.UpdatedAt = time.Now().UnixMilli()

	if err := s.store.UpsertCommandMetadata(r.Context(), meta); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save metadata")
		return
	}

	now := time.Now().UnixMilli()
	for _, label := range req.RemoveTags {
		if err := s.store.RemoveTag(r.Context(), cmd.ID, label); err != nil {
			s.logWarn("remove tag failed", "label", label, "err", err)
		}
	}
	for _, label := range req.AddTags {
		if err := s.store.AddTag(r.Context(), cmd.ID, label, now); err != nil && !errors.Is(err, db.ErrDuplicateTag) {
			s.logWarn("add tag failed", "label", label, "err", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

// handleGetSessions handles GET /api/sessions.
// Returns all panes ordered newest-first, each with their commands ordered by seq.
func (s *Server) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	panes, err := s.store.ListPanes(r.Context())
	if err != nil {
		s.logWarn("sessions: ListPanes failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	sessions := make([]SessionResponse, 0, len(panes))
	for _, p := range panes {
		cmds, err := s.store.ListCommandsByPane(r.Context(), p.ID)
		if err != nil {
			s.logWarn("sessions: ListCommandsByPane failed", "pane_id", p.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to list commands")
			return
		}
		commands := make([]CommandResponse, 0, len(cmds))
		for _, c := range cmds {
			commands = append(commands, commandToResponse(c))
		}
		sessions = append(sessions, SessionResponse{
			ID:       p.ID,
			Name:     p.ID,
			Commands: commands,
		})
	}
	writeJSON(w, http.StatusOK, sessions)
}

// handleGetTags handles GET /api/tags.
// Returns all tags joined with their parent command in a single query.
func (s *Server) handleGetTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListAllTagsWithCommands(r.Context())
	if err != nil {
		s.logWarn("tags: ListAllTagsWithCommands failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list tags")
		return
	}

	result := make([]TagResponse, 0, len(tags))
	for _, t := range tags {
		result = append(result, TagResponse{
			ID:          strconv.FormatInt(t.ID, 10),
			CommandID:   strconv.FormatInt(t.CommandID, 10),
			ShortID:     t.ShortID,
			Label:       t.Label,
			CommandText: t.CommandText,
			PaneID:      t.PaneID,
			CreatedAt:   time.UnixMilli(t.CreatedAt).UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetOutput handles GET /api/commands/{id}/output.
// Returns output_chunks concatenated, stripped of control sequences, as text/plain.
// SGR sequences (colours, bold, underline) are preserved; cursor movement, screen
// erase, OSC, and similar sequences are removed by scrub.Strip.
func (s *Server) handleGetOutput(w http.ResponseWriter, r *http.Request) {
	shortID := r.PathValue("id")
	if shortID == "" {
		writeError(w, http.StatusBadRequest, "missing command id")
		return
	}

	cmd, err := s.store.GetCommandByShortID(r.Context(), shortID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "command not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up command")
		return
	}

	chunks, err := s.store.GetOutputChunks(r.Context(), cmd.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load output")
		return
	}

	var raw []byte
	for _, chunk := range chunks {
		raw = append(raw, chunk.Data...)
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, scrub.Strip(raw))
}

// writableConfig defines the Viper groups and keys accessible via the config API.
// Keys not in this map are never read or written by the API — they can only be
// changed by editing the config file directly.
var writableConfig = map[string][]string{
	"ghostwriter": {"url", "token"},
	"sync":        {"interval_seconds"},
	"log":         {"level", "dir", "max_mb", "max_backups"},
	"cast":        {"dir"},
}

func buildConfig() map[string]map[string]any {
	cfg := make(map[string]map[string]any, len(writableConfig))
	for group, keys := range writableConfig {
		m := make(map[string]any, len(keys))
		for _, key := range keys {
			m[key] = viper.Get(group + "." + key)
		}
		cfg[group] = m
	}
	return cfg
}

// handleGetConfig handles GET /api/config.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildConfig())
}

// handleUpdateConfig handles POST /api/config.
// Only keys listed in writableConfig are accepted; unknown groups/keys are silently ignored.
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	for group, allowedKeys := range writableConfig {
		vals, ok := body[group]
		if !ok {
			continue
		}
		for _, key := range allowedKeys {
			if val, exists := vals[key]; exists {
				viper.Set(group+"."+key, val)
			}
		}
	}

	if err := viper.WriteConfig(); err != nil {
		s.logWarn("config: WriteConfig failed", "err", err)
		// Non-fatal — values updated in memory even if persist fails.
	}

	writeJSON(w, http.StatusOK, buildConfig())
}

// handleExportCast handles POST /api/cast.
// Synthesizes a single-command Asciinema .cast file and returns its path on disk.
func (s *Server) handleExportCast(w http.ResponseWriter, r *http.Request) {
	var req castRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	outDir := viper.GetString("cast.dir")
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(viper.GetString("db.path")), "casts")
	}

	path, err := cast.Synthesize(r.Context(), s.store, cast.Params{
		FirstShortID: req.ID,
		LastShortID:  req.ID,
		OutDir:       outDir,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		s.logWarn("cast: Synthesize failed", "id", req.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to export cast")
		return
	}

	writeJSON(w, http.StatusOK, castExportResponse{Success: true, Path: path})
}

// toolFromCommand extracts the first whitespace-delimited token from a shell
// command string as the tool name. "nmap -sC -sV 10.0.0.5" → "nmap".
func toolFromCommand(command string) string {
	for _, tok := range strings.Fields(command) {
		return tok
	}
	return ""
}

// commandToResponse converts a db.Command to the API wire shape.
// Duration is "-1" for in-flight commands (EndedAt == nil).
func commandToResponse(c *db.Command) CommandResponse {
	ts := time.UnixMilli(c.StartedAt).UTC().Format(time.RFC3339)
	dur := "-1"
	if c.EndedAt != nil {
		ms := *c.EndedAt - c.StartedAt
		dur = fmt.Sprintf("%.1fs", float64(ms)/1000.0)
	}
	return CommandResponse{
		ID:        c.ShortID,
		Command:   c.Command,
		Timestamp: ts,
		Duration:  dur,
		ExitCode:  c.ExitCode,
		PaneID:    c.PaneID,
	}
}

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

// handleHealth is a lightweight liveness probe. Returns 200 OK with no body.
// Used by `grim shell` to detect daemon outages during an active session.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
