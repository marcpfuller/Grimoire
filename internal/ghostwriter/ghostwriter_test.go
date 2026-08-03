package ghostwriter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/db"
	dbmocks "github.com/marcpfuller/grimoire/internal/db/mocks"
	"github.com/marcpfuller/grimoire/internal/ghostwriter"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
)

// finishedCmd is a fully-finalized command ready for pushing.
var finishedCmd = &db.Command{
	ID:        1,
	ShortID:   "a1b2c3d",
	PaneID:    "pane-01",
	ProjectID: "42",
	Command:   "ls -la /tmp",
	StartedAt: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC).UnixMilli(),
	EndedAt:   ptrInt64(time.Date(2026, 7, 24, 12, 0, 1, 0, time.UTC).UnixMilli()),
	ExitCode:  ptrInt(0),
}

// simpleChunks is a small set of output chunks with interleaved ANSI codes.
var simpleChunks = []*db.OutputChunk{
	{ID: 1, CommandID: 1, OffsetMs: 0, Data: []byte("\x1b[32mtotal 8\x1b[0m\n")},
	{ID: 2, CommandID: 1, OffsetMs: 50, Data: []byte("drwxr-xr-x  2 root root 4096 Jul 24 12:00 .\n")},
}

const validGQLResponse = `{"data":{"insert_oplogEntry_one":{"id":99}}}`

// newServer returns an httptest.Server that always responds with the given
// status code and body. It also records the last request seen.
func newServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

const testToken = "gw_test_token_abc123" //nolint:gosec // test credential only

// newTestClient creates a client pointing at url, pre-populates the oplog map
// for finishedCmd's project (42→7) via SetOplogByProjectForTest, and is ready
// to call PushCommand without Init.
func newTestClient(t *testing.T, store db.Store, url string, hc *http.Client) *ghostwriter.Client {
	t.Helper()
	c := ghostwriter.NewWithHTTP(store, url, testToken, hc, nil)
	ghostwriter.SetOplogByProjectForTest(c, 42, 7)
	return c
}

func TestPushCommand_HappyPath(t *testing.T) {
	srv, captured := newServer(t, http.StatusOK, validGQLResponse)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return(simpleChunks, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)
	require.NoError(t, err)

	// Verify auth header was sent.
	assert.Equal(t, "Bearer "+testToken, captured.Header.Get("Authorization"))
	assert.Equal(t, "application/json", captured.Header.Get("Content-Type"))
	assert.Equal(t, "/v1/graphql", captured.URL.Path)
}

func TestPushCommand_OutputAssemblyAndANSIStrip(t *testing.T) {
	// The pushed body's "variables.output" must be ANSI-stripped.
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validGQLResponse))
	}))
	t.Cleanup(srv.Close)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return(simpleChunks, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	require.NoError(t, client.PushCommand(context.Background(), finishedCmd))

	var payload struct {
		Variables struct {
			Output string `json:"output"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))

	// ANSI codes must be stripped.
	assert.NotContains(t, payload.Variables.Output, "\x1b[")
	// Plain text content must be preserved.
	assert.Contains(t, payload.Variables.Output, "total 8")
	assert.Contains(t, payload.Variables.Output, "drwxr-xr-x")
}

func TestPushCommand_HTTP401_ReturnsAuthError(t *testing.T) {
	srv, _ := newServer(t, http.StatusUnauthorized, `{"message":"Unauthorized"}`)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.ErrorAs(t, err, &authErr, "expected *grimsync.AuthError, got %T: %v", err, err)
}

func TestPushCommand_HTTP403_ReturnsAuthError(t *testing.T) {
	srv, _ := newServer(t, http.StatusForbidden, `{"message":"Forbidden"}`)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr)
}

func TestPushCommand_HasuraAccessDenied_ReturnsAuthError(t *testing.T) {
	body := `{"errors":[{"message":"access-denied","extensions":{"code":"access-denied"}}]}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr)
}

func TestPushCommand_HasuraNonAuthGraphQLError_ReturnsPlainError(t *testing.T) {
	body := `{"errors":[{"message":"field 'bogus' not found","extensions":{"code":"validation-failed"}}]}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.NotErrorAs(t, err, &authErr, "non-auth graphql error should NOT be AuthError")
}

func TestPushCommand_UnexpectedHTTPStatus_ReturnsPlainError(t *testing.T) {
	srv, _ := newServer(t, http.StatusInternalServerError, `internal server error`)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.NotErrorAs(t, err, &authErr)
}

func TestPushCommand_NetworkError_ReturnsPlainError(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := ghostwriter.NewWithHTTP(ms, "http://127.0.0.1:1", testToken, &http.Client{Timeout: 100 * time.Millisecond}, nil)
	ghostwriter.SetOplogByProjectForTest(client, 42, 7) // bypass Init; test only the push network error
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.NotErrorAs(t, err, &authErr)
}

func TestPushCommand_MissingToken_ReturnsErrorBeforeHTTP(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	// GetOutputChunks must NOT be called if token is missing.

	client := ghostwriter.NewWithHTTP(ms, "http://example.com", "", &http.Client{}, nil)
	ghostwriter.SetOplogByProjectForTest(client, 42, 1) // bypass Init; test only the token guard
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	require.Contains(t, err.Error(), "Token")
}

func TestPushCommand_InitNotCalled_ReturnsErrorBeforeHTTP(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	// Init never called → oplogByProject stays nil.

	client := ghostwriter.NewWithHTTP(ms, "http://example.com", "gw_tok", &http.Client{}, nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	require.Contains(t, err.Error(), "Init")
}

func TestPushCommand_NoOplogForProject_ReturnsError(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	// Map has no entry for project 42 (finishedCmd.ProjectID).
	client := ghostwriter.NewWithHTTP(ms, "http://example.com", "gw_tok", &http.Client{}, nil)
	ghostwriter.SetOplogByProjectForTest(client, 99, 5) // different project

	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	require.Contains(t, err.Error(), "project 42")
}

// ---- Init / oplog discovery tests ----

// discoverResponse is a single-oplog Init response: oplog 42 belongs to project 42.
const discoverResponse = `{"data":{"oplog":[{"id":42,"projectId":42}]}}`

func TestInit_HappyPath(t *testing.T) {
	// Server handles both the discover query and the subsequent push.
	call := 0
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		call++
		if call == 1 {
			// First request: oplog discovery.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(discoverResponse))
			return
		}
		// Second request: insert mutation.
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validGQLResponse))
	}))
	t.Cleanup(srv.Close)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return(simpleChunks, nil)

	client := ghostwriter.NewWithHTTP(ms, srv.URL, testToken, srv.Client(), nil)

	require.NoError(t, client.Init(context.Background()))
	require.NoError(t, client.PushCommand(context.Background(), finishedCmd))

	// Verify the discovered oplog_id (42) was used in the mutation.
	var payload struct {
		Variables struct {
			OplogID int `json:"oplog_id"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	require.Equal(t, 42, payload.Variables.OplogID, "Init should have discovered oplog_id=42")
}

func TestInit_NoOplog_ReturnsError(t *testing.T) {
	body := `{"data":{"oplog":[]}}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	client := ghostwriter.NewWithHTTP(ms, srv.URL, "gw_tok", srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no accessible oplog")
}

func TestInit_MultipleOplogs_BuildsMap(t *testing.T) {
	// Two oplogs belonging to two different projects — Init must succeed and
	// record both mappings.
	body := `{"data":{"oplog":[{"id":10,"projectId":1},{"id":20,"projectId":2}]}}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	client := ghostwriter.NewWithHTTP(ms, srv.URL, "gw_tok", srv.Client(), nil)

	require.NoError(t, client.Init(context.Background()))
}

func TestInit_AuthError_ReturnsAuthError(t *testing.T) {
	srv, _ := newServer(t, http.StatusUnauthorized, `{"message":"Unauthorized"}`)

	ms := dbmocks.NewMockStore(t)
	client := ghostwriter.NewWithHTTP(ms, srv.URL, "bad_token", srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.ErrorAs(t, err, &authErr)
}

func TestInit_HasuraAccessDenied_ReturnsAuthError(t *testing.T) {
	body := `{"errors":[{"message":"access-denied","extensions":{"code":"access-denied"}}]}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	client := ghostwriter.NewWithHTTP(ms, srv.URL, "gw_tok", srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	var authErr *grimsync.AuthError
	require.ErrorAs(t, err, &authErr)
}

func TestInit_IdempotentAfterSuccess(t *testing.T) {
	// A second call to Init must not make any additional HTTP requests.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(discoverResponse))
	}))
	t.Cleanup(srv.Close)

	ms := dbmocks.NewMockStore(t)
	client := ghostwriter.NewWithHTTP(ms, srv.URL, "gw_tok", srv.Client(), nil)

	require.NoError(t, client.Init(context.Background()))
	require.NoError(t, client.Init(context.Background()))
	require.Equal(t, 1, callCount, "second Init call should not make an HTTP request")
}

func TestPushCommand_NilEndedAt_OmitsEndDate(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validGQLResponse))
	}))
	t.Cleanup(srv.Close)

	cmdNoEnd := *finishedCmd
	cmdNoEnd.EndedAt = nil

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), cmdNoEnd.ID).Return([]*db.OutputChunk{}, nil)

	client := newTestClient(t, ms, srv.URL, srv.Client())
	require.NoError(t, client.PushCommand(context.Background(), &cmdNoEnd))

	var payload struct {
		Variables map[string]any `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	_, hasEndDate := payload.Variables["end_date"]
	assert.False(t, hasEndDate, "end_date should be absent when EndedAt is nil")
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"color reset", "foo\x1b[0mbar", "foobar"},
		{"bold green", "\x1b[1;32mOK\x1b[0m", "OK"},
		{"cursor move", "a\x1b[2Kb", "ab"},
		{"osc 133 activate", "x\x1b]133;C;id=1;start=1;sid=abc\x1b\\y", "xy"},
		{"osc 133 deactivate", "x\x1b]133;D\x1b\\y", "xy"},
		{"osc 0 title", "\x1b]0;mytitle\x07hello", "hello"},
		{"multi-line", "\x1b[32mfoo\x1b[0m\nbar\n", "foo\nbar\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ghostwriter.StripANSI(tt.input))
		})
	}
}

func TestGraphqlEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{"bare host", "http://localhost:8000", "http://localhost:8000/v1/graphql", false},
		{"trailing slash", "http://localhost:8000/", "http://localhost:8000/v1/graphql", false},
		{"path suffix from browser", "http://localhost:8000/users/admin/", "http://localhost:8000/v1/graphql", false},
		{"path suffix no trailing slash", "http://localhost:8000/users/admin", "http://localhost:8000/v1/graphql", false},
		{"https with port", "https://gw.example.com:8443/some/path", "https://gw.example.com:8443/v1/graphql", false},
		{"no scheme", "localhost:8000", "", true},
		{"empty", "", "", true},
		{"invalid url", "://bad", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghostwriter.GraphqlEndpoint(tt.rawURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---- helpers ----

func ptrInt64(v int64) *int64 { return &v }
func ptrInt(v int) *int       { return &v }
