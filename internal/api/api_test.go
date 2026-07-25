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

// --- helpers ---

func newServer(t *testing.T, store *dbmocks.MockStore, maxRetries int) *api.Server {
	t.Helper()
	return api.New(store, maxRetries, nil)
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
		wantStatus int
	}{
		{
			name:       "missing_pane_param",
			query:      "/api/next-id",
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "pane_not_registered",
			query: "/api/next-id?pane=unknown",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "unknown", mock.AnythingOfType("string")).
					Return(db.ErrNotFound).Once()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "reserve_non_conflict_error",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(errors.New("db down")).Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:  "conflict_then_success",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(db.ErrConflict).Once()
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(nil).Once()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "all_retries_exhausted",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(db.ErrConflict).Times(3)
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:  "success",
			query: "/api/next-id?pane=pane1",
			setupStore: func(m *dbmocks.MockStore) {
				m.On("ReserveShortID", mock.Anything, "pane1", mock.AnythingOfType("string")).
					Return(nil).Once()
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 3)

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
		name       string
		body       any
		setupStore func(*dbmocks.MockStore)
		wantStatus int
	}{
		{
			name:       "non_object_body",
			body:       "not-json",
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_pane_id",
			body:       map[string]any{"command": "ls", "started_at": int64(1000)},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_command",
			body:       map[string]any{"pane_id": "pane1", "started_at": int64(1000)},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_started_at",
			body:       map[string]any{"pane_id": "pane1", "command": "ls"},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "no_pending_reservation",
			body: map[string]any{"pane_id": "pane1", "command": "ls", "started_at": int64(1000)},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("CreateCommand", mock.Anything, mock.AnythingOfType("*db.Command")).
					Return(db.ErrNotFound).Once()
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "create_command_fails",
			body: map[string]any{"pane_id": "pane1", "command": "ls", "started_at": int64(1000)},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("CreateCommand", mock.Anything, mock.AnythingOfType("*db.Command")).
					Return(errors.New("db error")).Once()
			},
			wantStatus: http.StatusInternalServerError,
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
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/execute", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK {
				var resp struct {
					CommandID int64  `json:"command_id"`
					ShortID   string `json:"short_id"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				assert.Equal(t, int64(42), resp.CommandID)
				assert.Equal(t, "abc1234", resp.ShortID)
			}
		})
	}
}

// --- POST /api/precmd ---

func TestHandlePrecmd(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		setupStore func(*dbmocks.MockStore)
		wantStatus int
	}{
		{
			name:       "non_object_body",
			body:       "not-json",
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_pane_id",
			body:       map[string]any{"short_id": "abc1234", "ended_at": int64(2000), "exit_code": 0},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_short_id",
			body:       map[string]any{"pane_id": "pane1", "ended_at": int64(2000), "exit_code": 0},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_ended_at",
			body:       map[string]any{"pane_id": "pane1", "short_id": "abc1234", "exit_code": 0},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "command_not_found",
			body: map[string]any{"pane_id": "pane1", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 1},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("FinalizeCommand", mock.Anything, "abc1234", int64(2000), 1).
					Return(db.ErrNotFound).Once()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "finalize_fails",
			body: map[string]any{"pane_id": "pane1", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 1},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("FinalizeCommand", mock.Anything, "abc1234", int64(2000), 1).
					Return(errors.New("db error")).Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1", "short_id": "abc1234", "ended_at": int64(2000), "exit_code": 0},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("FinalizeCommand", mock.Anything, "abc1234", int64(2000), 0).
					Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/precmd", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// --- POST /api/deregister ---

func TestHandleDeregister(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		setupStore func(*dbmocks.MockStore)
		wantStatus int
	}{
		{
			name:       "non_object_body",
			body:       "not-json",
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_pane_id",
			body:       map[string]any{},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "store_fails",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("DeregisterPane", mock.Anything, "pane1").
					Return(errors.New("db error")).Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("DeregisterPane", mock.Anything, "pane1").Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/deregister", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// --- POST /api/register ---

func TestHandleRegister(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		setupStore func(*dbmocks.MockStore)
		wantStatus int
	}{
		{
			name:       "non_object_body",
			body:       "not-json",
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_pane_id",
			body:       map[string]any{"project_id": "proj1"},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_project_id",
			body:       map[string]any{"pane_id": "pane1"},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "upsert_fails",
			body: map[string]any{"pane_id": "pane1", "project_id": "proj1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", "proj1", mock.AnythingOfType("int64")).
					Return(errors.New("db error")).Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1", "project_id": "proj1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", "proj1", mock.AnythingOfType("int64")).
					Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 0)

			w := doRequest(t, srv, http.MethodPost, "/api/register", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
