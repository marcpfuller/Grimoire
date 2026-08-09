package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/api"
	"github.com/marcpfuller/grimoire/internal/db"
	dbmocks "github.com/marcpfuller/grimoire/internal/db/mocks"
	"github.com/marcpfuller/grimoire/internal/events"
	"github.com/marcpfuller/grimoire/internal/ghostwriter"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
)

// --- test doubles ---

// mockAPIPusher captures pushed commands for handleSync tests.
type mockAPIPusher struct {
	err    error
	pushed []*db.Command
}

func (m *mockAPIPusher) PushCommand(_ context.Context, cmd *db.Command) error {
	m.pushed = append(m.pushed, cmd)
	return m.err
}

// --- helpers ---

func newServer(t *testing.T, store *dbmocks.MockStore, maxRetries int) *api.Server {
	t.Helper()
	return api.New(store, maxRetries, nil, nil)
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
			body:       map[string]any{"oplog_id": 1},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing_oplog_id_defaults_zero",
			body: map[string]any{"pane_id": "pane1"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 0, "", mock.AnythingOfType("int64"), mock.AnythingOfType("int"), mock.AnythingOfType("int"), "").
					Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "upsert_fails",
			body: map[string]any{"pane_id": "pane1", "oplog_id": 1},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 1, "", mock.AnythingOfType("int64"), mock.AnythingOfType("int"), mock.AnythingOfType("int"), "").
					Return(errors.New("db error")).Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "name_too_long",
			body: map[string]any{"pane_id": "pane1", "name": "this-name-is-way-too-long-to-fit"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 0, "", mock.AnythingOfType("int64"), mock.AnythingOfType("int"), mock.AnythingOfType("int"), "this-name-is-way-too-long-to-fit").
					Return(db.ErrNameTooLong).Once()
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "success",
			body: map[string]any{"pane_id": "pane1", "oplog_id": 1},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 1, "", mock.AnythingOfType("int64"), mock.AnythingOfType("int"), mock.AnythingOfType("int"), "").
					Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "custom_name_passed_through",
			body: map[string]any{"pane_id": "pane1", "name": "my-pane"},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 0, "", mock.AnythingOfType("int64"), mock.AnythingOfType("int"), mock.AnythingOfType("int"), "my-pane").
					Return(nil).Once()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "zero_dimensions_default_to_220x50",
			body: map[string]any{"pane_id": "pane1", "oplog_id": 1, "width": 0, "height": 0},
			setupStore: func(m *dbmocks.MockStore) {
				m.On("UpsertPane", mock.Anything, "pane1", 1, "", mock.AnythingOfType("int64"), 220, 50, "").
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

// --- POST /api/tag ---

func TestHandleTag(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		setupStore func(*dbmocks.MockStore)
		wantStatus int
	}{
		{
			name:       "missing body fields",
			body:       map[string]string{},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing short_id",
			body:       map[string]string{"label": "Dumped hashes"},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing label",
			body:       map[string]string{"short_id": "a1b2c3d"},
			setupStore: func(_ *dbmocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "command not found",
			body: map[string]string{"short_id": "a1b2c3d", "label": "Dumped hashes"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(nil, db.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "duplicate tag",
			body: map[string]string{"short_id": "a1b2c3d", "label": "Dumped hashes"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42}, nil)
				m.EXPECT().AddTag(mock.Anything, int64(42), "Dumped hashes", mock.AnythingOfType("int64")).
					Return(db.ErrDuplicateTag)
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "store error on AddTag",
			body: map[string]string{"short_id": "a1b2c3d", "label": "Dumped hashes"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42}, nil)
				m.EXPECT().AddTag(mock.Anything, int64(42), "Dumped hashes", mock.AnythingOfType("int64")).
					Return(errors.New("disk full"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "happy path",
			body: map[string]string{"short_id": "a1b2c3d", "label": "Dumped hashes"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42}, nil)
				m.EXPECT().AddTag(mock.Anything, int64(42), "Dumped hashes", mock.AnythingOfType("int64")).
					Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			w := doRequest(t, newServer(t, store, 5), http.MethodPost, "/api/tag", tt.body)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandleTag_BroadcastsTagsUpdated(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	store.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
		Return(&db.Command{ID: 42}, nil)
	store.EXPECT().AddTag(mock.Anything, int64(42), "Dumped hashes", mock.AnythingOfType("int64")).
		Return(nil)

	broker := events.New()
	sub := broker.Subscribe()
	defer broker.Unsubscribe(sub)
	srv := api.New(store, 5, broker, nil)

	w := doRequest(t, srv, http.MethodPost, "/api/tag",
		map[string]string{"short_id": "a1b2c3d", "label": "Dumped hashes"})
	require.Equal(t, http.StatusNoContent, w.Code)

	select {
	case e := <-sub:
		assert.Equal(t, "tags_updated", e.Type)
		data, ok := e.Data.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "a1b2c3d", data["short_id"])
		assert.Equal(t, int64(42), data["command_id"])
		assert.Equal(t, "Dumped hashes", data["label"])
	case <-time.After(time.Second):
		t.Fatal("expected tags_updated event to be broadcast")
	}
}

// --- GET /api/commands/{id}/metadata ---

func TestHandleGetMetadata(t *testing.T) {
	tests := []struct {
		name        string
		setupStore  func(*dbmocks.MockStore)
		wantStatus  int
		wantDescrip string
	}{
		{
			name: "existing_metadata_description_is_unmodified",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(0)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(&db.CommandMetadata{CommandID: 42, Description: "operator note"}, nil)
				m.EXPECT().GetTagsByCommand(mock.Anything, int64(42)).Return(nil, nil)
			},
			wantStatus:  http.StatusOK,
			wantDescrip: "operator note",
		},
		{
			name: "no_metadata_row_clean_exit_defaults_to_command_executed",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(0)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().GetTagsByCommand(mock.Anything, int64(42)).Return(nil, nil)
			},
			wantStatus:  http.StatusOK,
			wantDescrip: "Command Executed",
		},
		{
			name: "no_metadata_row_nonzero_exit_defaults_to_command_failed",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(1)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().GetTagsByCommand(mock.Anything, int64(42)).Return(nil, nil)
			},
			wantStatus:  http.StatusOK,
			wantDescrip: "Command Failed",
		},
		{
			name: "no_metadata_row_negative_exit_defaults_to_command_failed",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(-1)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().GetTagsByCommand(mock.Anything, int64(42)).Return(nil, nil)
			},
			wantStatus:  http.StatusOK,
			wantDescrip: "Command Failed",
		},
		{
			name: "no_metadata_row_still_running_defaults_to_blank",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: nil}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().GetTagsByCommand(mock.Anything, int64(42)).Return(nil, nil)
			},
			wantStatus:  http.StatusOK,
			wantDescrip: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 5)

			w := doRequest(t, srv, http.MethodGet, "/api/commands/a1b2c3d/metadata", nil)

			require.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK {
				var resp api.MetadataResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.wantDescrip, resp.Description)
			}
		})
	}
}

func intPtr(i int) *int { return &i }

// --- PATCH /api/commands/{id}/metadata ---

func TestHandlePatchMetadata(t *testing.T) {
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
			name: "command_not_found",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(nil, db.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "lookup_error",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(nil, errors.New("db error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "metadata_not_found_creates_fresh_and_saves",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls"}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.MatchedBy(func(meta *db.CommandMetadata) bool {
					return meta.CommandID == 42 && meta.Operator == "alice"
				})).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "metadata_load_error",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls"}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, errors.New("db error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "metadata_not_found_persists_default_description_for_clean_exit",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(0)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.MatchedBy(func(meta *db.CommandMetadata) bool {
					return meta.Description == "Command Executed"
				})).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "metadata_not_found_persists_default_description_for_failed_exit",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(1)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.MatchedBy(func(meta *db.CommandMetadata) bool {
					return meta.Description == "Command Failed"
				})).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "metadata_not_found_explicit_description_overrides_default",
			body: map[string]any{"description": "custom note"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls", ExitCode: intPtr(0)}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(nil, db.ErrNotFound)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.MatchedBy(func(meta *db.CommandMetadata) bool {
					return meta.Description == "custom note"
				})).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "upsert_error",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls"}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(&db.CommandMetadata{CommandID: 42}, nil)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.Anything).
					Return(errors.New("db error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "no_tag_changes",
			body: map[string]any{"operator": "alice"},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls"}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(&db.CommandMetadata{CommandID: 42}, nil)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.Anything).
					Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "add_and_remove_tags",
			body: map[string]any{"addTags": []string{"new-tag"}, "removeTags": []string{"old-tag"}},
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
					Return(&db.Command{ID: 42, Command: "ls"}, nil)
				m.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
					Return(&db.CommandMetadata{CommandID: 42}, nil)
				m.EXPECT().UpsertCommandMetadata(mock.Anything, mock.Anything).
					Return(nil)
				m.EXPECT().RemoveTag(mock.Anything, int64(42), "old-tag").Return(nil)
				m.EXPECT().AddTag(mock.Anything, int64(42), "new-tag", mock.AnythingOfType("int64")).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			srv := newServer(t, store, 5)

			w := doRequest(t, srv, http.MethodPatch, "/api/commands/a1b2c3d/metadata", tt.body)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandlePatchMetadata_BroadcastsTagsUpdatedOnlyWhenTagsChange(t *testing.T) {
	tests := []struct {
		name          string
		body          any
		wantBroadcast bool
	}{
		{
			name:          "field_only_update_does_not_broadcast",
			body:          map[string]any{"operator": "alice"},
			wantBroadcast: false,
		},
		{
			name:          "add_tag_broadcasts",
			body:          map[string]any{"addTags": []string{"new-tag"}},
			wantBroadcast: true,
		},
		{
			name:          "remove_tag_broadcasts",
			body:          map[string]any{"removeTags": []string{"old-tag"}},
			wantBroadcast: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			store.EXPECT().GetCommandByShortID(mock.Anything, "a1b2c3d").
				Return(&db.Command{ID: 42, Command: "ls"}, nil)
			store.EXPECT().GetCommandMetadata(mock.Anything, int64(42)).
				Return(&db.CommandMetadata{CommandID: 42}, nil)
			store.EXPECT().UpsertCommandMetadata(mock.Anything, mock.Anything).Return(nil)
			store.EXPECT().RemoveTag(mock.Anything, int64(42), mock.Anything).Return(nil).Maybe()
			store.EXPECT().AddTag(mock.Anything, int64(42), mock.Anything, mock.AnythingOfType("int64")).Return(nil).Maybe()

			broker := events.New()
			sub := broker.Subscribe()
			defer broker.Unsubscribe(sub)
			srv := api.New(store, 5, broker, nil)

			w := doRequest(t, srv, http.MethodPatch, "/api/commands/a1b2c3d/metadata", tt.body)
			require.Equal(t, http.StatusNoContent, w.Code)

			select {
			case e := <-sub:
				if !tt.wantBroadcast {
					t.Fatalf("unexpected broadcast: %+v", e)
				}
				assert.Equal(t, "tags_updated", e.Type)
			case <-time.After(100 * time.Millisecond):
				if tt.wantBroadcast {
					t.Fatal("expected tags_updated event to be broadcast")
				}
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	w := doRequest(t, newServer(t, store, 5), http.MethodGet, "/api/health", "")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleGhostwriterHealth(t *testing.T) {
	tests := []struct {
		name          string
		check         func(context.Context) error
		wantStatus    int
		wantGWStatus  string
		forbiddenBody string
	}{
		{
			name:         "not configured",
			wantStatus:   http.StatusServiceUnavailable,
			wantGWStatus: "Unreachable",
		},
		{
			name: "connected",
			check: func(ctx context.Context) error {
				if _, hasDeadline := ctx.Deadline(); !hasDeadline {
					return errors.New("missing health-check deadline")
				}
				return nil
			},
			wantStatus:   http.StatusOK,
			wantGWStatus: "Connected",
		},
		{
			name: "upstream failure is generic",
			check: func(context.Context) error {
				return errors.New("sensitive upstream response")
			},
			wantStatus:    http.StatusServiceUnavailable,
			wantGWStatus:  "Unreachable",
			forbiddenBody: "sensitive upstream response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			srv := newServer(t, store, 5)
			srv.SetRuntime(api.RuntimeConfig{GhostwriterHealth: tt.check})

			w := doRequest(t, srv, http.MethodGet, "/api/ghostwriter/health", nil)
			require.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			body := w.Body.Bytes()
			var response struct {
				Status string `json:"status"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.Equal(t, tt.wantGWStatus, response.Status)
			if tt.forbiddenBody != "" {
				assert.NotContains(t, string(body), tt.forbiddenBody)
			}
		})
	}
}

func TestHandleDelete(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		storeCall  string
		id         string
		storeErr   error
		wantStatus int
	}{
		{name: "command_success", target: "/api/commands/a1b2c3d", storeCall: "DeleteCommandByShortID", id: "a1b2c3d", wantStatus: http.StatusNoContent},
		{name: "command_not_found", target: "/api/commands/missing", storeCall: "DeleteCommandByShortID", id: "missing", storeErr: db.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "command_store_error", target: "/api/commands/a1b2c3d", storeCall: "DeleteCommandByShortID", id: "a1b2c3d", storeErr: errors.New("db down"), wantStatus: http.StatusInternalServerError},
		{name: "pane_success", target: "/api/panes/pane-1", storeCall: "DeletePane", id: "pane-1", wantStatus: http.StatusNoContent},
		{name: "pane_not_found", target: "/api/panes/missing", storeCall: "DeletePane", id: "missing", storeErr: db.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "pane_store_error", target: "/api/panes/pane-1", storeCall: "DeletePane", id: "pane-1", storeErr: errors.New("db down"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			if tt.storeCall == "DeleteCommandByShortID" {
				store.On(tt.storeCall, mock.Anything, tt.id).
					Return(&db.DeleteCommandResult{PaneID: "pane-1"}, tt.storeErr).Once()
			} else {
				store.On(tt.storeCall, mock.Anything, tt.id).Return(tt.storeErr).Once()
			}

			w := doRequest(t, newServer(t, store, 5), http.MethodDelete, tt.target, nil)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandleDelete_BroadcastsRefreshEvents(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		storeCall   string
		id          string
		paneDeleted bool
		wantType    string
		wantPaneID  string
	}{
		{name: "command", target: "/api/commands/a1b2c3d", storeCall: "DeleteCommandByShortID", id: "a1b2c3d", wantType: "command_deleted"},
		{name: "last_command", target: "/api/commands/a1b2c3d", storeCall: "DeleteCommandByShortID", id: "a1b2c3d", paneDeleted: true, wantType: "session_deleted", wantPaneID: "pane-1"},
		{name: "pane", target: "/api/panes/pane-1", storeCall: "DeletePane", id: "pane-1", wantType: "session_deleted", wantPaneID: "pane-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			if tt.storeCall == "DeleteCommandByShortID" {
				store.On(tt.storeCall, mock.Anything, tt.id).
					Return(&db.DeleteCommandResult{PaneID: "pane-1", PaneDeleted: tt.paneDeleted}, nil).Once()
			} else {
				store.On(tt.storeCall, mock.Anything, tt.id).Return(nil).Once()
			}
			broker := events.New()
			sub := broker.Subscribe()
			defer broker.Unsubscribe(sub)
			srv := api.New(store, 5, broker, nil)

			w := doRequest(t, srv, http.MethodDelete, tt.target, nil)
			require.Equal(t, http.StatusNoContent, w.Code)

			select {
			case event := <-sub:
				assert.Equal(t, tt.wantType, event.Type)
				if tt.wantPaneID != "" {
					data, ok := event.Data.(map[string]any)
					require.True(t, ok)
					assert.Equal(t, tt.wantPaneID, data["pane_id"])
				}
			case <-time.After(time.Second):
				t.Fatalf("expected %s event", tt.wantType)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/sessions
// ---------------------------------------------------------------------------

func TestHandleGetSessions(t *testing.T) {
	completedAt := int64(2500)
	exitCode := 0

	tests := []struct {
		name       string
		setupStore func(*dbmocks.MockStore)
		wantStatus int
		check      func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "list_panes_fails",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "no_panes_returns_empty_array",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var got []any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
				assert.Empty(t, got)
			},
		},
		{
			name: "list_commands_fails",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{{ID: "pane1"}}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").
					Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "pane_with_no_commands_serialises_as_empty_array_not_null",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{{ID: "pane1"}}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").Return([]*db.Command{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				// Must be [] in JSON, not null — frontend does .map() on this field.
				assert.Contains(t, w.Body.String(), `"commands":[]`)
			},
		},
		{
			name: "inflight_command_duration_is_sentinel_minus_one",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{{ID: "pane1"}}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").Return([]*db.Command{
					{ID: 1, ShortID: "abc0001", PaneID: "pane1", Command: "nmap", StartedAt: 1000, EndedAt: nil},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var sessions []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
				cmds := sessions[0]["commands"].([]any)
				require.Len(t, cmds, 1)
				assert.Equal(t, "-1", cmds[0].(map[string]any)["duration"])
			},
		},
		{
			name: "completed_command_duration_formatted_in_seconds",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{{ID: "pane1"}}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").Return([]*db.Command{
					// EndedAt=2500, StartedAt=1000 → 1500 ms → "1.5s"
					{ID: 1, ShortID: "abc0001", PaneID: "pane1", Command: "nmap",
						StartedAt: 1000, EndedAt: &completedAt, ExitCode: &exitCode},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var sessions []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
				cmds := sessions[0]["commands"].([]any)
				assert.Equal(t, "1.5s", cmds[0].(map[string]any)["duration"])
			},
		},
		{
			name: "session_name_uses_pane_name_not_id",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{{ID: "pane-abc123", Name: "quiet-star-c123"}}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane-abc123").Return([]*db.Command{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var sessions []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
				require.Len(t, sessions, 1)
				assert.Equal(t, "pane-abc123", sessions[0]["id"])
				assert.Equal(t, "quiet-star-c123", sessions[0]["name"])
			},
		},
		{
			name: "multiple_panes_all_queried",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{
					{ID: "pane1"}, {ID: "pane2"},
				}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").Return([]*db.Command{}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane2").Return([]*db.Command{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var sessions []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
				assert.Len(t, sessions, 2)
			},
		},
		{
			name: "second_pane_command_error_aborts_with_500",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListPanes(mock.Anything).Return([]*db.Pane{
					{ID: "pane1"}, {ID: "pane2"},
				}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane1").Return([]*db.Command{}, nil)
				m.EXPECT().ListCommandsByPane(mock.Anything, "pane2").Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			w := doRequest(t, newServer(t, store, 0), http.MethodGet, "/api/sessions", nil)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/tags
// ---------------------------------------------------------------------------

func TestHandleGetTags(t *testing.T) {
	tests := []struct {
		name       string
		setupStore func(*dbmocks.MockStore)
		wantStatus int
		check      func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "store_fails",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListAllTagsWithCommands(mock.Anything).Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "no_tags_returns_empty_array",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListAllTagsWithCommands(mock.Anything).Return([]*db.TagWithCommand{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var got []any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
				assert.Empty(t, got)
			},
		},
		{
			name: "integer_ids_serialised_as_strings",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListAllTagsWithCommands(mock.Anything).Return([]*db.TagWithCommand{
					{ID: 7, CommandID: 42, ShortID: "abc0001", Label: "recon",
						CommandText: "nmap -sC 10.0.0.1", PaneID: "pane1", CreatedAt: 1_700_000_000_000},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var got []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
				require.Len(t, got, 1)
				assert.Equal(t, "7", got[0]["id"])
				assert.Equal(t, "42", got[0]["commandId"])
			},
		},
		{
			name: "response_fields_match_source_fields",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().ListAllTagsWithCommands(mock.Anything).Return([]*db.TagWithCommand{
					{ID: 1, CommandID: 10, ShortID: "def0002", Label: "lateral-movement",
						CommandText: "ssh user@10.0.0.5", PaneID: "pane-xyz", CreatedAt: 0},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var got []map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
				tag := got[0]
				assert.Equal(t, "def0002", tag["shortId"])
				assert.Equal(t, "lateral-movement", tag["label"])
				assert.Equal(t, "ssh user@10.0.0.5", tag["commandText"])
				assert.Equal(t, "pane-xyz", tag["paneId"])
				assert.NotEmpty(t, tag["createdAt"]) // RFC3339 string
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			w := doRequest(t, newServer(t, store, 0), http.MethodGet, "/api/tags", nil)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/commands/{id}/output
// ---------------------------------------------------------------------------

func TestHandleGetOutput(t *testing.T) {
	tests := []struct {
		name       string
		shortID    string
		setupStore func(*dbmocks.MockStore)
		wantStatus int
		check      func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:    "command_not_found",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(nil, db.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:    "get_command_fails",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:    "get_chunks_fails",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(&db.Command{ID: 1}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(1)).Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:    "no_chunks_returns_200_empty_body_with_text_content_type",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(&db.Command{ID: 1}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(1)).Return([]*db.OutputChunk{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
				assert.Empty(t, w.Body.Bytes())
			},
		},
		{
			name:    "multiple_chunks_concatenated_in_order",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(&db.Command{ID: 1}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(1)).Return([]*db.OutputChunk{
					{Data: []byte("hello ")},
					{Data: []byte("world\n")},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
				assert.Equal(t, "hello world\n", w.Body.String())
			},
		},
		{
			name:    "raw_ansi_bytes_passed_through_unmodified",
			shortID: "abc0001",
			setupStore: func(m *dbmocks.MockStore) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(&db.Command{ID: 1}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(1)).Return([]*db.OutputChunk{
					{Data: []byte("\x1b[1;32mok\x1b[0m")},
				}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Equal(t, []byte("\x1b[1;32mok\x1b[0m"), w.Body.Bytes())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store)
			req := httptest.NewRequest(http.MethodGet, "/api/commands/"+tt.shortID+"/output", nil)
			w := httptest.NewRecorder()
			newServer(t, store, 0).Handler().ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/config
// ---------------------------------------------------------------------------

func TestHandleGetConfig(t *testing.T) {
	viper.Set("ghostwriter.url", "https://gw.example.com")
	viper.Set("ghostwriter.tokens", []string{"tok-abc123", "tok-def456"})
	viper.Set("sync.interval_seconds", 60)
	viper.Set("log.level", "debug")

	store := dbmocks.NewMockStore(t)
	w := doRequest(t, newServer(t, store, 0), http.MethodGet, "/api/config", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var cfg map[string]map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&cfg))

	// All expected groups present.
	for _, group := range []string{"ghostwriter", "sync", "log", "cast"} {
		assert.Contains(t, cfg, group, "missing group %q", group)
	}

	// Spot-check that values reflect what was set in Viper.
	assert.Equal(t, "https://gw.example.com", cfg["ghostwriter"]["url"])
	assert.Equal(t, "debug", cfg["log"]["level"])

	// token key must not be present (replaced by tokens).
	assert.NotContains(t, cfg["ghostwriter"], "token")

	// Non-whitelisted keys absent (db.path, server.host, etc.).
	assert.NotContains(t, cfg, "db")
	assert.NotContains(t, cfg, "server")
	assert.NotContains(t, cfg, "api")
}

// ---------------------------------------------------------------------------
// POST /api/config
// ---------------------------------------------------------------------------

func TestHandleUpdateConfig(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		check      func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:       "bad_json",
			body:       "not-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty_body_returns_current_config_unchanged",
			body:       map[string]any{},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result struct {
					Config  map[string]any `json:"config"`
					Changed []string       `json:"changed"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
				assert.Contains(t, result.Config, "ghostwriter")
				assert.Contains(t, result.Config, "sync")
			},
		},
		{
			name: "known_key_reflected_in_response",
			body: map[string]any{
				"ghostwriter": map[string]any{"url": "https://updated.example.com"},
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result struct {
					Config  map[string]map[string]any `json:"config"`
					Changed []string                  `json:"changed"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
				assert.Equal(t, "https://updated.example.com", result.Config["ghostwriter"]["url"])
			},
		},
		{
			name: "unknown_group_silently_ignored",
			body: map[string]any{
				"danger": map[string]any{"exec": "rm -rf /"},
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var cfg map[string]any
				require.NoError(t, json.NewDecoder(w.Body).Decode(&cfg))
				assert.NotContains(t, cfg, "danger")
			},
		},
		{
			name: "unknown_key_within_known_group_silently_ignored",
			body: map[string]any{
				"ghostwriter": map[string]any{"injected_field": "evil"},
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result struct {
					Config  map[string]map[string]any `json:"config"`
					Changed []string                  `json:"changed"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
				assert.NotContains(t, result.Config["ghostwriter"], "injected_field")
			},
		},
		{
			name: "write_config_failure_is_non_fatal",
			// viper.WriteConfig() always fails in tests (no config file loaded).
			// The handler must still return 200 with the in-memory updated value.
			body:       map[string]any{"sync": map[string]any{"interval_seconds": 999}},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result struct {
					Config  map[string]map[string]any `json:"config"`
					Changed []string                  `json:"changed"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
				assert.InDelta(t, float64(999), result.Config["sync"]["interval_seconds"], 0.001)
			},
		},
		{
			name: "multiple_groups_updated_atomically",
			body: map[string]any{
				"ghostwriter": map[string]any{"url": "https://multi.example.com", "auto_sync": true},
				"log":         map[string]any{"level": "warn"},
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result struct {
					Config  map[string]map[string]any `json:"config"`
					Changed []string                  `json:"changed"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
				assert.Equal(t, "https://multi.example.com", result.Config["ghostwriter"]["url"])
				assert.Equal(t, "warn", result.Config["log"]["level"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := dbmocks.NewMockStore(t)
			w := doRequest(t, newServer(t, store, 0), http.MethodPost, "/api/config", tt.body)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// POST /api/cast
// ---------------------------------------------------------------------------

func TestHandleExportCast(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		setupStore func(*dbmocks.MockStore, string)
		wantStatus int
		check      func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:       "bad_json",
			body:       "not-json",
			setupStore: func(_ *dbmocks.MockStore, _ string) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_ids_field",
			body:       map[string]any{},
			setupStore: func(_ *dbmocks.MockStore, _ string) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "command_not_found",
			body: map[string]any{"ids": []string{"abc0001"}},
			setupStore: func(m *dbmocks.MockStore, _ string) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(nil, db.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "store_error_returns_500",
			body: map[string]any{"ids": []string{"abc0001"}},
			setupStore: func(m *dbmocks.MockStore, _ string) {
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(nil, errors.New("db down"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success_single_id_writes_cast_file_and_returns_path",
			body: map[string]any{"ids": []string{"abc0001"}},
			setupStore: func(m *dbmocks.MockStore, tmpDir string) {
				viper.Set("cast.dir", tmpDir)
				cmd := &db.Command{
					ID: 1, ShortID: "abc0001", PaneID: "pane1",
					Seq: 1, Command: "ls -la", StartedAt: 1_000_000,
				}
				m.EXPECT().GetCommandByShortID(mock.Anything, "abc0001").Return(cmd, nil)
				m.EXPECT().GetPane(mock.Anything, "pane1").Return(
					&db.Pane{ID: "pane1", Width: 220, Height: 50}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(1)).
					Return([]*db.OutputChunk{}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp struct {
					Success bool   `json:"success"`
					Path    string `json:"path"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				assert.True(t, resp.Success)
				assert.NotEmpty(t, resp.Path)
				assert.FileExists(t, resp.Path)
			},
		},
		{
			name: "success_multiple_ids_writes_cast_file",
			body: map[string]any{"ids": []string{"def0002", "def0005"}},
			setupStore: func(m *dbmocks.MockStore, tmpDir string) {
				viper.Set("cast.dir", tmpDir)
				cmd2 := &db.Command{
					ID: 2, ShortID: "def0002", PaneID: "pane2",
					Seq: 2, Command: "id", StartedAt: 2_000_000,
				}
				cmd5 := &db.Command{
					ID: 5, ShortID: "def0005", PaneID: "pane2",
					Seq: 5, Command: "whoami", StartedAt: 5_000_000,
				}
				m.EXPECT().GetCommandByShortID(mock.Anything, "def0002").Return(cmd2, nil)
				m.EXPECT().GetCommandByShortID(mock.Anything, "def0005").Return(cmd5, nil)
				m.EXPECT().GetPane(mock.Anything, "pane2").Return(
					&db.Pane{ID: "pane2", Width: 80, Height: 24}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(2)).
					Return([]*db.OutputChunk{{Data: []byte("uid=0(root)\n")}}, nil)
				m.EXPECT().GetOutputChunks(mock.Anything, int64(5)).
					Return([]*db.OutputChunk{{Data: []byte("root\n")}}, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp struct {
					Success bool   `json:"success"`
					Path    string `json:"path"`
				}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				assert.True(t, resp.Success)
				assert.FileExists(t, resp.Path)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			store := dbmocks.NewMockStore(t)
			tt.setupStore(store, tmpDir)
			w := doRequest(t, newServer(t, store, 0), http.MethodPost, "/api/cast", tt.body)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.check != nil {
				tt.check(t, w)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// POST /api/shutdown
// ---------------------------------------------------------------------------

func TestHandleShutdown_triggers_shutdown(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	srv := newServer(t, store, 5)

	called := false
	srv.SetRuntime(api.RuntimeConfig{
		Shutdown: func() { called = true },
	})

	w := doRequest(t, srv, http.MethodPost, "/api/shutdown", nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called, "Shutdown func must be called")
}

func TestHandleShutdown_no_handler_returns_503(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	srv := newServer(t, store, 5)
	// No Shutdown func set in RuntimeConfig.

	w := doRequest(t, srv, http.MethodPost, "/api/shutdown", nil)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --------------------------------------------------
// POST /api/sync
// --------------------------------------------------

func newSyncerForAPI(t *testing.T, store *dbmocks.MockStore, pusher grimsync.Pusher) *grimsync.Syncer {
	t.Helper()
	return grimsync.New(store, pusher, grimsync.Config{Interval: time.Second}, nil)
}

func TestHandleSync_oplog_override_pushes_with_override_id(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockAPIPusher{}
	cmd := &db.Command{ID: 5, ShortID: "abc123", OplogID: 0, SyncedGW: false}

	store.On("GetCommandByShortID", mock.Anything, "abc123").Return(cmd, nil).Once()
	store.On("MarkCommandSynced", mock.Anything, int64(5), 42, int64(0)).Return(nil).Once()

	srv := newServer(t, store, 0)
	srv.SetRuntime(api.RuntimeConfig{Syncer: newSyncerForAPI(t, store, pusher)})

	w := doRequest(t, srv, http.MethodPost, "/api/sync",
		map[string]any{"short_id": "abc123", "oplog_id": 42})

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, pusher.pushed, 1)
	assert.Equal(t, 42, pusher.pushed[0].OplogID, "override oplog ID must reach the pusher")
}

func TestHandleSync_no_oplog_id_returns_400(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	pusher := &mockAPIPusher{}
	cmd := &db.Command{ID: 6, ShortID: "nooplog", OplogID: 0, SyncedGW: false}

	store.On("GetCommandByShortID", mock.Anything, "nooplog").Return(cmd, nil).Once()

	srv := newServer(t, store, 0)
	srv.SetRuntime(api.RuntimeConfig{Syncer: newSyncerForAPI(t, store, pusher)})

	w := doRequest(t, srv, http.MethodPost, "/api/sync",
		map[string]string{"short_id": "nooplog"})

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, pusher.pushed, "nothing must be pushed when oplog ID is absent")
}

func TestHandleRegister_oplog_id_and_name_pass_through(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	store.On("UpsertPane", mock.Anything, "pane1", 42, "BASILISK Oplog", mock.AnythingOfType("int64"),
		mock.AnythingOfType("int"), mock.AnythingOfType("int"), "").Return(nil).Once()

	srv := newServer(t, store, 0)

	w := doRequest(t, srv, http.MethodPost, "/api/register",
		map[string]any{"pane_id": "pane1", "oplog_id": 42, "oplog_name": "BASILISK Oplog"})

	require.Equal(t, http.StatusNoContent, w.Code)
}

// ---------------------------------------------------------------------------
// GET /api/oplogs
// ---------------------------------------------------------------------------

func TestHandleGetOplogs_OplogsCallbackNil_Returns503(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	srv := newServer(t, store, 0)
	// No runtime set → Oplogs callback is nil.

	w := doRequest(t, srv, http.MethodGet, "/api/oplogs", nil)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleGetOplogs_EmptyList_ReturnsEmptyArray(t *testing.T) {
	store := dbmocks.NewMockStore(t)
	srv := newServer(t, store, 0)
	srv.SetRuntime(api.RuntimeConfig{
		Oplogs: func() []ghostwriter.OplogEntry { return nil },
	})

	w := doRequest(t, srv, http.MethodGet, "/api/oplogs", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var result []ghostwriter.OplogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestHandleGetOplogs_ReturnsOplogs(t *testing.T) {
	oplogs := []ghostwriter.OplogEntry{
		{ID: 1, Name: "Alpha Oplog", ProjectID: 10},
		{ID: 2, Name: "Beta Oplog", ProjectID: 20},
	}
	store := dbmocks.NewMockStore(t)
	srv := newServer(t, store, 0)
	srv.SetRuntime(api.RuntimeConfig{
		Oplogs: func() []ghostwriter.OplogEntry { return oplogs },
	})

	w := doRequest(t, srv, http.MethodGet, "/api/oplogs", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var result []ghostwriter.OplogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result, 2)
	assert.Equal(t, 1, result[0].ID)
	assert.Equal(t, "Alpha Oplog", result[0].Name)
	assert.Equal(t, 10, result[0].ProjectID)
	assert.Equal(t, 2, result[1].ID)
	assert.Equal(t, "Beta Oplog", result[1].Name)
}
