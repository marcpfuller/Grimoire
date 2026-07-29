package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// daemonClient returns an *http.Client and the resolved bearer token for
// making authenticated requests to the local daemon. The token is read from
// the file pointed to by auth.token_path (defaults to ~/.grim/token).
func daemonToken() (string, error) {
	path := viper.GetString("auth.token_path")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("apiclient: home dir: %w", err)
		}
		path = home + "/.grim/token"
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from config/home dir, not user input
	if err != nil {
		return "", fmt.Errorf("apiclient: read token %s: %w", path, err)
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", fmt.Errorf("apiclient: token file %s is empty", path)
	}
	return tok, nil
}

// apiGet performs an authenticated GET against the daemon and returns the
// response. The caller is responsible for closing resp.Body.
func apiGet(urlPath string) (*http.Response, error) {
	tok, err := daemonToken()
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("http://%s:%d%s",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
		urlPath,
	)
	req, err := http.NewRequest(http.MethodGet, addr, nil) //nolint:gosec // G107: addr is localhost from config
	if err != nil {
		return nil, fmt.Errorf("apiclient: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return http.DefaultClient.Do(req)
}

// apiPost performs an authenticated POST with a JSON body against the daemon
// and returns the response. The caller is responsible for closing resp.Body.
func apiPost(urlPath string, body any) (*http.Response, error) {
	tok, err := daemonToken()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("apiclient: marshal body: %w", err)
	}
	addr := fmt.Sprintf("http://%s:%d%s",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
		urlPath,
	)
	req, err := http.NewRequest(http.MethodPost, addr, bytes.NewReader(encoded)) //nolint:gosec // G107: addr is localhost from config
	if err != nil {
		return nil, fmt.Errorf("apiclient: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}
