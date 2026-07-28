// Package auth manages the daemon's bearer token: generating it on first start,
// persisting it to ~/.grim/token (mode 0600), and providing the HTTP middleware
// that enforces it on every API request.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// tokenPath returns the canonical path for the token file.
func tokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("auth: home dir: %w", err)
	}
	return filepath.Join(home, ".grim", "token"), nil
}

// ResolvePath returns path if non-empty, otherwise returns the default token
// path (~/.grim/token). Useful when the caller needs the concrete filesystem
// path without loading the token itself — for example, to pass it to a child
// process via an environment variable.
func ResolvePath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return tokenPath()
}

// LoadOrCreate reads the token from path. If the file does not exist or is
// empty, a new 32-byte (64 hex char) cryptographically random token is
// generated, written with mode 0600, and returned.
//
// The parent directory is created with mode 0700 if absent.
// If path is empty, the default (~/.grim/token) is used.
func LoadOrCreate(path string) (string, error) {
	if path == "" {
		var err error
		path, err = tokenPath()
		if err != nil {
			return "", err
		}
	}

	// Try to read an existing token first.
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: path comes from config or os.UserHomeDir(), not user input
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}

	// Generate a new token.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	tok := hex.EncodeToString(buf)

	// Ensure the directory exists with restricted permissions.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(path), err)
	}

	// Write with owner-only read/write (0600). No other user or process can
	// read the token file.
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("auth: write token to %s: %w", path, err)
	}

	return tok, nil
}

// Load reads the token from path without generating one. Returns an error if
// the file is absent or empty — this typically means the daemon has not been
// started yet.
//
// If path is empty, the default (~/.grim/token) is used.
func Load(path string) (string, error) {
	if path == "" {
		var err error
		path, err = tokenPath()
		if err != nil {
			return "", err
		}
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path comes from config or os.UserHomeDir(), not user input
	if err != nil {
		return "", fmt.Errorf("auth: read token from %s: %w — is the daemon running?", path, err)
	}
	if tok := strings.TrimSpace(string(data)); tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("auth: token file %s is empty", path)
}

// Middleware returns an HTTP middleware that enforces bearer token
// authentication. Requests whose Authorization header does not match
// "Bearer <token>" receive a 401 response. The comparison is performed in
// constant time to prevent timing oracle attacks.
//
// The /api/health endpoint is exempt so the watchDaemon probe and external
// liveness checks continue to work without a token.
func Middleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health probe is intentionally unauthenticated — it reveals no
			// sensitive data and is needed by the grim shell watcher.
			if r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}

			const prefix = "Bearer "
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, prefix) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="grimoire"`)
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			got := authHeader[len(prefix):]
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="grimoire"`)
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
