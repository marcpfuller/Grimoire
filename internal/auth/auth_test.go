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

// overrideHome temporarily sets HOME so tokenPath() resolves inside a temp dir.
func overrideHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestLoadOrCreate(t *testing.T) {
	t.Run("creates_token_on_first_call", func(t *testing.T) {
		overrideHome(t)
		tok, err := auth.LoadOrCreate()
		require.NoError(t, err)
		assert.Len(t, tok, 64, "token must be 64 hex chars (32 bytes)")
	})

	t.Run("token_file_has_0600_permissions", func(t *testing.T) {
		home := overrideHome(t)
		_, err := auth.LoadOrCreate()
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(home, ".grim", "token"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
			"token file must be owner-read/write only")
	})

	t.Run("parent_dir_has_0700_permissions", func(t *testing.T) {
		home := overrideHome(t)
		_, err := auth.LoadOrCreate()
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(home, ".grim"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(),
			"~/.grim directory must be owner-only")
	})

	t.Run("idempotent_returns_same_token", func(t *testing.T) {
		overrideHome(t)
		tok1, err := auth.LoadOrCreate()
		require.NoError(t, err)
		tok2, err := auth.LoadOrCreate()
		require.NoError(t, err)
		assert.Equal(t, tok1, tok2, "second call must return the persisted token")
	})

	t.Run("token_is_hex_encoded", func(t *testing.T) {
		overrideHome(t)
		tok, err := auth.LoadOrCreate()
		require.NoError(t, err)
		for _, c := range tok {
			assert.Contains(t, "0123456789abcdef", string(c),
				"token must be lowercase hex")
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("error_when_no_file_exists", func(t *testing.T) {
		overrideHome(t)
		_, err := auth.Load()
		assert.Error(t, err)
	})

	t.Run("returns_token_written_by_LoadOrCreate", func(t *testing.T) {
		overrideHome(t)
		created, err := auth.LoadOrCreate()
		require.NoError(t, err)
		loaded, err := auth.Load()
		require.NoError(t, err)
		assert.Equal(t, created, loaded)
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
