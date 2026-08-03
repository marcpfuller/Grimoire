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
	"context"
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
	"github.com/marcpfuller/grimoire/internal/events"
	"github.com/marcpfuller/grimoire/internal/scrub"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
	"github.com/marcpfuller/grimoire/internal/util"
)

// RuntimeConfig holds the live-updatable daemon tunables the API can change
// without a daemon restart. All fields are optional — nil means no-op.
type RuntimeConfig struct {
	// Syncer is the running sync loop. SetInterval and SetPusher are called
	// when the matching config keys change.
	Syncer *grimsync.Syncer

	// LevelVar controls the JSON log sink's minimum level at runtime.
	LevelVar *slog.LevelVar

	// NewPusher builds and initialises a Ghostwriter pusher for the given
	// url/token pair. Returning a nil Pusher (or an error) prevents the swap.
	// When url or token is empty it should return a no-op pusher, not an error.
	NewPusher func(ctx context.Context, url, token string) (grimsync.Pusher, error)
}

// Server holds the shared dependencies for all HTTP handlers.
type Server struct {
	store             db.Store
	maxShortIDRetries int
	broker            *events.Broker
	logger            *slog.Logger
	runtime           RuntimeConfig
}

// New creates a Server. broker and logger may be nil — broadcasts and log
// calls become no-ops when their respective fields are nil.
func New(store db.Store, maxShortIDRetries int, broker *events.Broker, logger *slog.Logger) *Server {
	return &Server{
		store:             store,
		maxShortIDRetries: maxShortIDRetries,
		broker:            broker,
		logger:            logger,
	}
}

// SetRuntime wires the live-updatable daemon tunables into the server after
// construction. Call once before serving requests.
func (s *Server) SetRuntime(cfg RuntimeConfig) {
	s.runtime = cfg
}

// broadcast sends e to the event broker if one is configured.
func (s *Server) broadcast(e events.Event) {
	if s.broker != nil {
		s.broker.Broadcast(e)
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
	mux.HandleFunc("POST /api/sync", s.handleSync)
	mux.HandleFunc("GET /api/events", s.handleEvents)
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
	Prompt    string `json:"prompt"`
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
	IDs []string `json:"ids"`
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
	s.broadcast(events.Event{
		Type: "session_registered",
		Data: map[string]any{"pane_id": req.PaneID, "project_id": req.ProjectID},
	})
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
		Prompt:    req.Prompt,
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
	s.broadcast(events.Event{
		Type: "command_started",
		Data: map[string]any{"command_id": cmd.ID, "short_id": cmd.ShortID, "pane_id": req.PaneID},
	})
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
	s.broadcast(events.Event{
		Type: "command_finished",
		Data: map[string]any{"short_id": req.ShortID, "pane_id": req.PaneID, "exit_code": req.ExitCode},
	})
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
	s.broadcast(events.Event{
		Type: "session_deregistered",
		Data: map[string]any{"pane_id": req.PaneID},
	})
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
	// Prepend the shell prompt and echoed command so the UI shows the full
	// terminal context. For commands recorded before this field was added,
	// Prompt is empty and the output chunks are returned as-is.
	if cmd.Prompt != "" {
		raw = append(raw, []byte(cmd.Prompt+cmd.Command+"\r\n")...)
	}
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
	"ghostwriter": {"url", "token", "auto_sync"},
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

// updateConfigResponse is returned by POST /api/config.
type updateConfigResponse struct {
	Config  map[string]map[string]any `json:"config"`
	Changed []string                  `json:"changed"`
}

// handleGetConfig handles GET /api/config.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildConfig())
}

// handleUpdateConfig handles POST /api/config.
// Only keys listed in writableConfig are accepted; unknown groups/keys are silently ignored.
// Changed keys take effect immediately: sync interval resets the ticker, log level
// updates the live slog.LevelVar, and ghostwriter credentials swap the pusher.
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var changed []string
	gwChanged := false

	for group, allowedKeys := range writableConfig {
		vals, ok := body[group]
		if !ok {
			continue
		}
		for _, key := range allowedKeys {
			val, exists := vals[key]
			if !exists {
				continue
			}
			dotKey := group + "." + key
			old := viper.Get(dotKey)
			viper.Set(dotKey, val)
			if fmt.Sprint(old) != fmt.Sprint(val) {
				changed = append(changed, dotKey)
			}
		}
	}

	if err := viper.WriteConfig(); err != nil {
		s.logWarn("config: WriteConfig failed", "err", err)
		// Non-fatal — values updated in memory even if persist fails.
	}

	// Apply runtime side-effects for each changed key.
	for _, key := range changed {
		switch key {
		case "log.level":
			if s.runtime.LevelVar != nil {
				var lvl slog.Level
				_ = lvl.UnmarshalText([]byte(viper.GetString("log.level")))
				s.runtime.LevelVar.Set(lvl)
				s.logInfo("config: log level changed", "level", lvl.String())
			}
		case "sync.interval_seconds":
			if s.runtime.Syncer != nil {
				d := time.Duration(viper.GetInt("sync.interval_seconds")) * time.Second
				s.runtime.Syncer.SetInterval(d)
				s.logInfo("config: sync interval changed", "interval", d.String())
			}
		case "ghostwriter.auto_sync":
			if s.runtime.Syncer != nil {
				enabled := viper.GetBool("ghostwriter.auto_sync")
				s.runtime.Syncer.SetAutoSync(enabled)
				s.logInfo("config: auto_sync changed", "auto_sync", enabled)
			}
		case "ghostwriter.url", "ghostwriter.token":
			gwChanged = true
		}
	}

	if gwChanged && s.runtime.Syncer != nil && s.runtime.NewPusher != nil {
		url := viper.GetString("ghostwriter.url")
		tok := viper.GetString("ghostwriter.token")
		pusher, err := s.runtime.NewPusher(r.Context(), url, tok)
		if err != nil {
			s.logWarn("config: ghostwriter pusher swap failed", "err", err)
			writeError(w, http.StatusInternalServerError, "ghostwriter pusher init failed: "+err.Error())
			return
		}
		s.runtime.Syncer.SetPusher(pusher)
		s.logInfo("config: ghostwriter pusher swapped", "url", url)
	}

	if changed == nil {
		changed = []string{}
	}
	writeJSON(w, http.StatusOK, updateConfigResponse{Config: buildConfig(), Changed: changed})
}

// handleExportCast handles POST /api/cast.
// Synthesizes an Asciinema .cast file from the requested command IDs and returns its path on disk.
func (s *Server) handleExportCast(w http.ResponseWriter, r *http.Request) {
	var req castRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}

	outDir := viper.GetString("cast.dir")
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(viper.GetString("db.path")), "casts")
	}

	path, err := cast.Synthesize(r.Context(), s.store, cast.Params{
		ShortIDs: req.IDs,
		OutDir:   outDir,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "command not found")
			return
		}
		s.logWarn("cast: Synthesize failed", "ids", req.IDs, "err", err)
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
		SyncedGW:  c.SyncedGW,
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

func (s *Server) logInfo(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Info(msg, args...)
	}
}

// handleHealth is a lightweight liveness probe. Returns 200 OK with no body.
// Used by `grim shell` to detect daemon outages during an active session.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleSync handles POST /api/sync.
// It pushes a single unsynced command to Ghostwriter immediately, regardless
// of the auto_sync setting. The request body must be JSON with a "short_id"
// field. Returns 404 if the command is not found, 409 if already synced.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShortID string `json:"short_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.ShortID == "" {
		writeError(w, http.StatusBadRequest, "short_id is required")
		return
	}
	if s.runtime.Syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "sync not available")
		return
	}
	if err := s.runtime.Syncer.PushOne(r.Context(), req.ShortID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "command not found: "+req.ShortID)
			return
		}
		// "already synced" is a conflict, anything else is a server error.
		if strings.Contains(err.Error(), "already synced") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"synced": req.ShortID})
}

// handleEvents streams server-sent events to the caller over a long-lived
// HTTP connection. Each event has an "event:" type line and a "data:" JSON
// payload. A ": ping" comment is sent every 25 seconds to prevent proxies
// and load balancers from closing idle connections.
//
// The browser EventSource API cannot send custom request headers, so the
// auth middleware (auth.Middleware) also accepts the bearer token via the
// "?token=" query parameter for this endpoint only.
//
// Event types: command_started, command_finished, session_registered,
// session_deregistered.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported by this server")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering

	if s.broker == nil {
		// No broker configured — write a single comment and return.
		fmt.Fprintf(w, ": events not available\n\n") //nolint:errcheck
		flusher.Flush()
		return
	}

	ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(ch)

	// 25-second keepalive — below the 30-second idle timeout used by most
	// reverse proxies. The ": ping" comment is valid SSE and ignored by clients.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n") //nolint:errcheck
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(e.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data) //nolint:errcheck
			flusher.Flush()
		}
	}
}
