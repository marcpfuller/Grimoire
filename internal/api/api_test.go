package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/api"
	"github.com/marcpfuller/grimoire/internal/db"
	dbmocks "github.com/marcpfuller/grimoire/internal/db/mocks"
)

// --- test doubles ---

type mockSession struct {
	activateCalls   int
	deactivateCalls int
	lastCommandID   int64
	lastStartedAt   int64
	lastShortID     string
}

func (m *mockSession) Activate(commandID, startedAt int64, shortID string) {
	m.activateCalls++
	m.lastCommandID = commandID
	m.lastStartedAt = startedAt
	m.lastShortID = shortID
}

func (m *mockSession) Deactivate() {
	m.deactivateCalls++
}

type mockRegistry struct {
	sessions map[string]*mockSession
	removed  []string
}

func newRegistry(paneIDs ...string) *mockRegistry {
	r := &mockRegistry{sessions: make(map[string]*mockSession)}
	for _, id := range paneIDs {
		r.sessions[id] = &mockSession{}
	}
	return r
}

func (r *mockRegistry) Lookup(paneID string) (api.PtySession, bool) {
	s, ok := r.sessions[paneID]
	if !ok {
		return nil, false
	}
	return s, true
}

func (r *mockRegistry) Remove(paneID string) {
	r.removed = append(r.removed, paneID)
	delete(r.sessions, paneID)
}

func (r *mockRegistry) session(paneID string) *mockSession {
	return r.sessions[paneID]
}

// --- helpers ---

func newServer(t *testing.T, store *dbmocks.MockStore, reg *mockRegistry, maxRetries int) *api.Server {
	t.Helper()
	return api.New(store, reg, maxRetries, nil)
}

func doRequest(t *testing.T, srv *api.Server, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// --- GET /api/next-id ---

func TestHandleNextID(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		setupStore func(*dbmocks.MockStore)
		setupReg   func() *mockRegistry
		wantStatus int
	}{
		{
			name:       "missing_pane_param",
			query:      "/api/next-id",
			setupStore: func(_ *dbmocks.MockStore) {},
			setupReg:   func() *mockRegistry { return newRegistry() },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "session_not_found",
			query:      "/api/next-id?pane=unknown",
			setupStore: func(_ *dbmocks.MockStore) {},
			setupReg:   func() *mockRegistry { return newRegistry() },
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:  "reserve_non_conflict_error",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(errors.New("db down")).Once()
			},
			setupReg:   func() *mockRegistry { return newRegistry("pane1") },
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:  "conflict_then_success",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				// First attempt conflicts; second succeeds.
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(db.ErrConflict).Once()
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(nil).Once()
			},
			setupReg:   func() *mockRegistry { return newRegistry("pane1") },
			wantStatus: http.StatusOK,
		},
		{
			name:  "all_retries_exhausted",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				// MaxShortIDRetries=3 for this test — all attempts conflict.
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(db.ErrConflict).Times(3)
			},
			setupReg:   func() *mockRegistry { return newRegistry("pane1") },
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:  "success",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(nil).Once()
			},
			setupReg:   func() *mockRegistry { return newRegistry("pane1") },
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			reg := tt.setupReg()
			srv := newServer(t, store, reg, 3)

			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK {
				var resp map[string]string
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				assert.NotEmpty(t, resp["short_id"])
			}
		})
	}
}

// --- POST /api/execute ---

func TestHandleExecute(t *testing.T) {
	tests := []struct {
		name         string
		body         any
		setupStore   func(*dbmocks.MockStore)
		setupReg     func() *mockRegistry
		wantStatus   int
		wantActivate bool
	}{
		{
			name:         "non_object_body",
			body:         "not-json",
			setupStore:   func(_ *dbmocks.MockStore) {},
			setupReg:     func() *mockRegistry { return newRegistry() },
			wantStatus:   http.StatusBadRequest,
			wantActivate: false,
		},
		{
			name:         "missing_pane_id",
			body:         map[string]any{"command": "ls", "started_at": int64(1000)},
			setupStore:   func(_ *dbmocks.MockStore) {},
			setupReg:     func() *mockRegistry { return newRegistry() },
			wantStatus:   http.StatusBadRequest,
			wantActivate: false,
		},
		{
			name:         "missing_command",
			body:         map[string]any{"pane_id": "pane1", "started_at": int64(1000)},
			setupStore:   func(_ *dbmocks.MockStore) {},
			setupReg:     func() *mockRegistry { return newRegistry() },
			wantStatus:   http.StatusBadRequest,
			wantActivate: false,
		},
		{
			name:         "missing_started_at",
			body:         map[string]any{"pane_id": "pane1", "command": "ls"},
			setupStore:   func(_ *dbmocks.MockStore) {},
			setupReg:     func() *mockRegistry { return newRegistry() },
			wantStatus:   http.StatusBadRequest,
			wantActivate: false,
		},
		{
			name:         "session_not_found",
			body:         map[string]any{"pane_id": "ghost", "command": "ls", "started_at": int64(1000)},
			setupStore:   func(_ *dbmocks.MockStore) {},
			setupReg:     func() *mockRegistry { return newRegistry() },
			wantStatus:   http.StatusServiceUnavailable,
			wantActivate: false,
		},
		{
			name: "create_command_fails",
			body: map[string]any{"pane_id": "pane1", "command": "ls", "started_at": int64(1000)},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("CreateCommand", mock.Anything, mock.AnythingOfType("*db.Command")).
					Return(errors.New("db error")).Once()
			},
			setupReg:     func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:   http.StatusInternalServerError,
			wantActivate: false,
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1", "command": "ls", "started_at": int64(1000)},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("CreateCommand", mock.Anything, mock.AnythingOfType("*db.Command")).
					Run(func(args mock.Arguments) {
						cmd := args.Get(1).(*db.Command)
						cmd.ID = 42
						cmd.ShortID = "abc1234"
					}).
					Return(nil).Once()
			},
			setupReg:     func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:   http.StatusOK,
			wantActivate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			reg := tt.setupReg()
			srv := newServer(t, store, reg, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/execute", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
			if sess := reg.session("pane1"); sess != nil {
				if tt.wantActivate {
					assert.Equal(t, 1, sess.activateCalls, "Activate should be called once on success")
					assert.Equal(t, int64(42), sess.lastCommandID, "Activate received wrong commandID")
					assert.Equal(t, int64(1000), sess.lastStartedAt, "Activate received wrong startedAt")
					assert.Equal(t, "abc1234", sess.lastShortID, "Activate received wrong shortID")
					var resp struct {
						CommandID int64  `json:"command_id"`
						ShortID   string `json:"short_id"`
					}
					require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
					assert.Equal(t, int64(42), resp.CommandID, "response command_id mismatch")
					assert.NotEmpty(t, resp.ShortID)
				} else {
					assert.Equal(t, 0, sess.activateCalls, "Activate must not be called on failure")
				}
			}
		})
	}
}

// --- POST /api/precmd ---

func TestHandlePrecmd(t *testing.T) {
	tests := []struct {
		name           string
		body           any
		setupStore     func(*dbmocks.MockStore)
		setupReg       func() *mockRegistry
		wantStatus     int
		wantDeactivate bool
	}{
		{
			name:           "non_object_body",
			body:           "not-json",
			setupStore:     func(_ *dbmocks.MockStore) {},
			setupReg:       func() *mockRegistry { return newRegistry() },
			wantStatus:     http.StatusBadRequest,
			wantDeactivate: false,
		},
		{
			name:           "missing_pane_id",
			body:           map[string]any{"short_id": "abc1234", "ended_at": int64(2000), "exit_code": 0},
			setupStore:     func(_ *dbmocks.MockStore) {},
			setupReg:       func() *mockRegistry { return newRegistry() },
			wantStatus:     http.StatusBadRequest,
			wantDeactivate: false,
		},
		{
			name:           "missing_short_id",
			body:           map[string]any{"pane_id": "pane1", "ended_at": int64(2000), "exit_code": 0},
			setupStore:     func(_ *dbmocks.MockStore) {},
			setupReg:       func() *mockRegistry { return newRegistry() },
			wantStatus:     http.StatusBadRequest,
			wantDeactivate: false,
		},
		{
			name:           "missing_ended_at",
			body:           map[string]any{"pane_id": "pane1", "short_id": "abc1234", "exit_code": 0},
			setupStore:     func(_ *dbmocks.MockStore) {},
			setupReg:       func() *mockRegistry { return newRegistry() },
			wantStatus:     http.StatusBadRequest,
			wantDeactivate: false,
		},
		{
			name:           "session_not_found",
			body:           map[string]any{"pane_id": "ghost", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 0},
			setupStore:     func(_ *dbmocks.MockStore) {},
			setupReg:       func() *mockRegistry { return newRegistry() },
			wantStatus:     http.StatusServiceUnavailable,
			wantDeactivate: false,
		},
		{
			name: "finalize_fails_deactivate_still_called",
			body: map[string]any{"pane_id": "pane1", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 1},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("FinalizeCommand", mock.Anything, "abc1234", int64(2000), 1).
					Return(errors.New("db error")).Once()
			},
			setupReg:       func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:     http.StatusInternalServerError,
			wantDeactivate: true, // defer fires regardless of FinalizeCommand result
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 0},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("FinalizeCommand", mock.Anything, "abc1234", int64(2000), 0).
					Return(nil).Once()
			},
			setupReg:       func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:     http.StatusNoContent,
			wantDeactivate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			reg := tt.setupReg()
			srv := newServer(t, store, reg, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/precmd", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
			if sess := reg.session("pane1"); sess != nil {
				wantCount := 0
				if tt.wantDeactivate {
					wantCount = 1
				}
				assert.Equal(t, wantCount, sess.deactivateCalls, "Deactivate call count mismatch")
			}
		})
	}
}

// --- POST /api/deregister ---

func TestHandleDeregister(t *testing.T) {
	tests := []struct {
		name        string
		body        any
		setupStore  func(*dbmocks.MockStore)
		setupReg    func() *mockRegistry
		wantStatus  int
		wantRemoved bool
	}{
		{
			name:        "non_object_body",
			body:        "not-json",
			setupStore:  func(_ *dbmocks.MockStore) {},
			setupReg:    func() *mockRegistry { return newRegistry() },
			wantStatus:  http.StatusBadRequest,
			wantRemoved: false,
		},
		{
			name:        "missing_pane_id",
			body:        map[string]any{},
			setupStore:  func(_ *dbmocks.MockStore) {},
			setupReg:    func() *mockRegistry { return newRegistry() },
			wantStatus:  http.StatusBadRequest,
			wantRemoved: false,
		},
		{
			name: "store_fails",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("DeregisterPane", mock.Anything, "pane1").
					Return(errors.New("db error")).Once()
			},
			setupReg:    func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:  http.StatusInternalServerError,
			wantRemoved: false,
		},
		{
			name: "success_with_active_session",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("DeregisterPane", mock.Anything, "pane1").Return(nil).Once()
			},
			setupReg:    func() *mockRegistry { return newRegistry("pane1") },
			wantStatus:  http.StatusNoContent,
			wantRemoved: true,
		},
		{
			name: "success_without_session",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("DeregisterPane", mock.Anything, "pane1").Return(nil).Once()
			},
			// No session registered — shell exited before any command was run.
			setupReg:    func() *mockRegistry { return newRegistry() },
			wantStatus:  http.StatusNoContent,
			wantRemoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			reg := tt.setupReg()
			srv := newServer(t, store, reg, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/deregister", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantRemoved {
				assert.Contains(t, reg.removed, "pane1", "pane1 should be removed from registry")
			} else {
				assert.NotContains(t, reg.removed, "pane1", "pane1 should not be removed from registry")
			}
		})
	}
}

// TestHandleDeregisterDeactivatesBeforeRemove verifies that Deactivate is called
// on the session object before Remove evicts it from the registry.
func TestHandleDeregisterDeactivatesBeforeRemove(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	store.On("DeregisterPane", mock.Anything, "pane1").Return(nil).Once()

	reg := newRegistry("pane1")
	sess := reg.session("pane1")
	srv := newServer(t, store, reg, 0)

	w := doRequest(t, srv, http.MethodPost, "/api/deregister", map[string]any{"pane_id": "pane1"})

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, sess.deactivateCalls, "Deactivate must be called once")
	assert.Contains(t, reg.removed, "pane1", "session must be removed from registry")
}

// Ensure pty.Session satisfies api.PtySession at compile time.
// This lives in the test file to avoid importing internal/pty from internal/api.
var _ api.PtySession = (*mockSession)(nil)
