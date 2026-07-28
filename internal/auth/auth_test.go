package auth_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marcpfuller/grimoire/internal/auth"
)

// tokenFile returns a path inside a fresh temp dir for use as the token file.
// The parent directory is created so tests can verify its permissions.
func tokenFile(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".grim")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	return filepath.Join(dir, "token")
}

func TestLoadOrCreate(t *testing.T) {
	t.Run("creates_token_on_first_call", func(t *testing.T) {
		tok, err := auth.LoadOrCreate(tokenFile(t))
		require.NoError(t, err)
		assert.Len(t, tok, 64, "token must be 64 hex chars (32 bytes)")
	})

	t.Run("token_file_has_0600_permissions", func(t *testing.T) {
		path := tokenFile(t)
		_, err := auth.LoadOrCreate(path)
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
			"token file must be owner-read/write only")
	})

	t.Run("parent_dir_has_0700_permissions", func(t *testing.T) {
		path := tokenFile(t)
		_, err := auth.LoadOrCreate(path)
		require.NoError(t, err)

		info, err := os.Stat(filepath.Dir(path))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(),
			"token directory must be owner-only")
	})

	t.Run("idempotent_returns_same_token", func(t *testing.T) {
		path := tokenFile(t)
		tok1, err := auth.LoadOrCreate(path)
		require.NoError(t, err)
		tok2, err := auth.LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, tok1, tok2, "second call must return the persisted token")
	})

	t.Run("token_is_hex_encoded", func(t *testing.T) {
		tok, err := auth.LoadOrCreate(tokenFile(t))
		require.NoError(t, err)
		for _, c := range tok {
			assert.Contains(t, "0123456789abcdef", string(c),
				"token must be lowercase hex")
		}
	})

	t.Run("empty_path_uses_default", func(t *testing.T) {
		// Pass "" — should fall back to ~/.grim/token without error.
		// We can't assert the path, but we can assert no error and a valid token.
		tok, err := auth.LoadOrCreate("")
		require.NoError(t, err)
		assert.Len(t, tok, 64)
	})
}

func TestLoad(t *testing.T) {
	t.Run("error_when_no_file_exists", func(t *testing.T) {
		_, err := auth.Load(filepath.Join(t.TempDir(), "nonexistent", "token"))
		assert.Error(t, err)
	})

	t.Run("returns_token_written_by_LoadOrCreate", func(t *testing.T) {
		path := tokenFile(t)
		created, err := auth.LoadOrCreate(path)
		require.NoError(t, err)
		loaded, err := auth.Load(path)
		require.NoError(t, err)
		assert.Equal(t, created, loaded)
	})

	t.Run("empty_path_uses_default", func(t *testing.T) {
		// Ensure a token exists at the default path first.
		_, err := auth.LoadOrCreate("")
		require.NoError(t, err)
		tok, err := auth.Load("")
		require.NoError(t, err)
		assert.Len(t, tok, 64)
	})
}

func TestMiddleware(t *testing.T) {
	const token = "abc123testtoken"

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(token)(inner)

	t.Run("valid_token_passes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("missing_token_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, `Bearer realm="grimoire"`, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("wrong_token_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer wrongtoken")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("no_bearer_prefix_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		req.Header.Set("Authorization", token) // no "Bearer " prefix
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("health_endpoint_exempt_from_auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		// No token at all.
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
