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

const validGQLResponse = `{"data":{"insert_oplog_oplogentry_one":{"id":99}}}`

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

// validCfg returns a Config pointing at the given server URL with sane defaults.
func validCfg(url string) ghostwriter.Config {
	return ghostwriter.Config{
		URL:     url,
		Token:   "gw_test_token_abc123",
		OplogID: 7,
	}
}

func TestPushCommand_HappyPath(t *testing.T) {
	srv, captured := newServer(t, http.StatusOK, validGQLResponse)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return(simpleChunks, nil)

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)
	require.NoError(t, err)

	// Verify auth header was sent.
	assert.Equal(t, "Bearer gw_test_token_abc123", captured.Header.Get("Authorization"))
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

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
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

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr, "expected *grimsync.AuthError, got %T: %v", err, err)
}

func TestPushCommand_HTTP403_ReturnsAuthError(t *testing.T) {
	srv, _ := newServer(t, http.StatusForbidden, `{"message":"Forbidden"}`)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr)
}

func TestPushCommand_HasuraAccessDenied_ReturnsAuthError(t *testing.T) {
	body := `{"errors":[{"message":"access-denied","extensions":{"code":"access-denied"}}]}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr)
}

func TestPushCommand_HasuraNonAuthGraphQLError_ReturnsPlainError(t *testing.T) {
	body := `{"errors":[{"message":"field 'bogus' not found","extensions":{"code":"validation-failed"}}]}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.NotErrorAs(t, err, &authErr, "non-auth graphql error should NOT be AuthError")
}

func TestPushCommand_UnexpectedHTTPStatus_ReturnsPlainError(t *testing.T) {
	srv, _ := newServer(t, http.StatusInternalServerError, `internal server error`)

	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.NotErrorAs(t, err, &authErr)
}

func TestPushCommand_NetworkError_ReturnsPlainError(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	ms.On("GetOutputChunks", context.Background(), finishedCmd.ID).Return([]*db.OutputChunk{}, nil)

	cfg := validCfg("http://127.0.0.1:1") // nothing listening on port 1
	client := ghostwriter.NewWithHTTP(ms, cfg, &http.Client{Timeout: 100 * time.Millisecond}, nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.NotErrorAs(t, err, &authErr)
}

func TestPushCommand_MissingToken_ReturnsErrorBeforeHTTP(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	// GetOutputChunks must NOT be called if config is invalid.

	cfg := ghostwriter.Config{URL: "http://example.com", Token: "", OplogID: 1}
	client := ghostwriter.NewWithHTTP(ms, cfg, &http.Client{}, nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Token")
}

func TestPushCommand_InitNotCalled_ReturnsErrorBeforeHTTP(t *testing.T) {
	ms := dbmocks.NewMockStore(t)
	// OplogID=0 and Init never called → resolvedOplogID stays 0.

	cfg := ghostwriter.Config{URL: "http://example.com", Token: "gw_tok", OplogID: 0}
	client := ghostwriter.NewWithHTTP(ms, cfg, &http.Client{}, nil)
	err := client.PushCommand(context.Background(), finishedCmd)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Init")
}

// ---- Init / oplog discovery tests ----

const discoverResponse = `{"data":{"oplog_oplog":[{"id":42}]}}`

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

	cfg := ghostwriter.Config{URL: srv.URL, Token: "gw_test_token_abc123"}
	client := ghostwriter.NewWithHTTP(ms, cfg, srv.Client(), nil)

	require.NoError(t, client.Init(context.Background()))
	require.NoError(t, client.PushCommand(context.Background(), finishedCmd))

	// Verify the discovered oplog_id (42) was used in the mutation.
	var payload struct {
		Variables struct {
			OplogID int `json:"oplog_id"`
		} `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, 42, payload.Variables.OplogID, "Init should have discovered oplog_id=42")
}

func TestInit_NoOplog_ReturnsError(t *testing.T) {
	body := `{"data":{"oplog_oplog":[]}}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	cfg := ghostwriter.Config{URL: srv.URL, Token: "gw_tok"}
	client := ghostwriter.NewWithHTTP(ms, cfg, srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no accessible oplog")
}

func TestInit_MultipleOplogs_ReturnsError(t *testing.T) {
	body := `{"data":{"oplog_oplog":[{"id":1},{"id":2}]}}`
	srv, _ := newServer(t, http.StatusOK, body)

	ms := dbmocks.NewMockStore(t)
	cfg := ghostwriter.Config{URL: srv.URL, Token: "gw_tok"}
	client := ghostwriter.NewWithHTTP(ms, cfg, srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 oplogs")
}

func TestInit_AuthError_ReturnsAuthError(t *testing.T) {
	srv, _ := newServer(t, http.StatusUnauthorized, `{"message":"Unauthorized"}`)

	ms := dbmocks.NewMockStore(t)
	cfg := ghostwriter.Config{URL: srv.URL, Token: "bad_token"}
	client := ghostwriter.NewWithHTTP(ms, cfg, srv.Client(), nil)

	err := client.Init(context.Background())
	require.Error(t, err)
	var authErr *grimsync.AuthError
	assert.ErrorAs(t, err, &authErr)
}

func TestInit_NoopWhenOplogIDPreset(t *testing.T) {
	// When OplogID is set in Config, Init must not make any HTTP calls.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ms := dbmocks.NewMockStore(t)
	cfg := ghostwriter.Config{URL: srv.URL, Token: "gw_tok", OplogID: 7}
	client := ghostwriter.NewWithHTTP(ms, cfg, srv.Client(), nil)

	require.NoError(t, client.Init(context.Background()))
	assert.Equal(t, 0, callCount, "Init should not make HTTP calls when OplogID is pre-set")
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

	client := ghostwriter.NewWithHTTP(ms, validCfg(srv.URL), srv.Client(), nil)
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

// ---- helpers ----

func ptrInt64(v int64) *int64 { return &v }
func ptrInt(v int) *int       { return &v }
