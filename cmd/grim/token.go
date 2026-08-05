package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/marcpfuller/grimoire/internal/ghostwriter"
)

// maskToken returns an abbreviated form of a service token safe to display in
// a terminal: the first 8 characters, "...", and the last 4 characters.
// Tokens of 12 characters or fewer are returned unchanged.
func maskToken(tok string) string {
	if len(tok) <= 12 {
		return tok
	}
	return tok[:8] + "..." + tok[len(tok)-4:]
}

// ---------------------------------------------------------------------------
// Ghostwriter per-token discovery
// ---------------------------------------------------------------------------

// discoverOplogsForToken queries Ghostwriter GraphQL directly using the given
// token and returns the oplogs visible to it. The caller provides the raw GW
// base URL (e.g. "https://gw.example.com"); the GraphQL endpoint is derived
// via ghostwriter.GraphqlEndpoint.
func discoverOplogsForToken(gwURL, token string) ([]oplogEntry, error) {
	endpoint, err := ghostwriter.GraphqlEndpoint(gwURL)
	if err != nil {
		return nil, fmt.Errorf("discoverOplogsForToken: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"query": "{ oplog { id projectId name } }"})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload)) //nolint:gosec // endpoint derived from config, not user input
	if err != nil {
		return nil, fmt.Errorf("discoverOplogsForToken: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discoverOplogsForToken: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var result struct {
		Data struct {
			Oplog []struct {
				ID        int    `json:"id"`
				ProjectID int    `json:"projectId"`
				Name      string `json:"name"`
			} `json:"oplog"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("discoverOplogsForToken: parse response: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("discoverOplogsForToken: graphql: %s", result.Errors[0].Message)
	}

	entries := make([]oplogEntry, len(result.Data.Oplog))
	for i, o := range result.Data.Oplog {
		entries[i] = oplogEntry{ID: o.ID, Name: o.Name, ProjectID: o.ProjectID}
	}
	return entries, nil
}

// ---------------------------------------------------------------------------
// Append
// ---------------------------------------------------------------------------

// tokenAppendDeps holds injectable dependencies for runAppendToken.
type tokenAppendDeps struct {
	getConfig  func() (gwURL string, tokens []string, err error)
	postTokens func(tokens []string) error
	stdout     io.Writer
}

// runAppendToken adds newToken to the ghostwriter.tokens list if not already
// present, then persists via postTokens.
func runAppendToken(newToken string, deps tokenAppendDeps) error {
	_, current, err := deps.getConfig()
	if err != nil {
		return fmt.Errorf("config append: %w", err)
	}

	for _, t := range current {
		if t == newToken {
			w := deps.stdout
			if w == nil {
				w = os.Stdout
			}
			_, _ = fmt.Fprintln(w, "no change (token already present)")
			return nil
		}
	}

	updated := append(current, newToken)
	if err := deps.postTokens(updated); err != nil {
		return fmt.Errorf("config append: %w", err)
	}

	w := deps.stdout
	if w == nil {
		w = os.Stdout
	}
	_, _ = fmt.Fprintf(w, "updated: ghostwriter.tokens (%d token(s))\n", len(updated))
	return nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// tokenSelectOption is one entry in the huh selector for --del.
type tokenSelectOption struct {
	label string // display text shown to the user
	token string // full token value (the value returned on selection)
}

// tokenDeleteDeps holds injectable dependencies for runDeleteToken.
type tokenDeleteDeps struct {
	// getConfig returns the Ghostwriter base URL and current token list.
	getConfig func() (gwURL string, tokens []string, err error)

	// discoverFn queries Ghostwriter directly for the oplogs a token can see.
	// May return (nil, err) for unavailable tokens; caller must handle gracefully.
	discoverFn func(gwURL, token string) ([]oplogEntry, error)

	// selectFn presents the interactive picker and returns the full token to delete.
	// Should return huh.ErrUserAborted when the user cancels.
	selectFn func(opts []tokenSelectOption) (string, error)

	// postTokens persists the updated token list.
	postTokens func(tokens []string) error

	// stderr receives warning messages. Defaults to os.Stderr when nil.
	stderr io.Writer
}

func (d *tokenDeleteDeps) warn(format string, a ...any) {
	w := d.stderr
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, "grim: warning: "+format+"\n", a...)
}

// runDeleteToken presents a huh selector of configured service tokens (each
// labelled with the oplogs it owns), then removes the chosen token from the
// list and persists via postTokens.
func runDeleteToken(deps tokenDeleteDeps) error {
	gwURL, tokens, err := deps.getConfig()
	if err != nil {
		return fmt.Errorf("config delete: %w", err)
	}
	if len(tokens) == 0 {
		return fmt.Errorf("config delete: no service tokens configured")
	}

	// Build selector options by querying Ghostwriter per-token.
	opts := make([]tokenSelectOption, len(tokens))
	for i, tok := range tokens {
		label := maskToken(tok)
		if deps.discoverFn != nil {
			if oplogs, discErr := deps.discoverFn(gwURL, tok); discErr != nil {
				deps.warn("could not discover oplogs for token %s (%v)", maskToken(tok), discErr)
				label += "  (unavailable)"
			} else if len(oplogs) > 0 {
				names := make([]string, len(oplogs))
				for j, o := range oplogs {
					names[j] = fmt.Sprintf("%s (id %d)", o.Name, o.ID)
				}
				label += "  →  " + strings.Join(names, ", ")
			}
		}
		opts[i] = tokenSelectOption{label: label, token: tok}
	}

	chosen, selErr := deps.selectFn(opts)
	if selErr != nil {
		if errors.Is(selErr, huh.ErrUserAborted) {
			return nil
		}
		return fmt.Errorf("config delete: selector: %w", selErr)
	}

	// Remove the chosen token from the list.
	updated := make([]string, 0, len(tokens)-1)
	for _, t := range tokens {
		if t != chosen {
			updated = append(updated, t)
		}
	}

	if err := deps.postTokens(updated); err != nil {
		return fmt.Errorf("config delete: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Real implementations wired in config.go
// ---------------------------------------------------------------------------

// realGetGWConfig reads ghostwriter.url and ghostwriter.tokens from the
// running daemon via GET /api/config.
func realGetGWConfig() (gwURL string, tokens []string, err error) {
	resp, err := apiGet("/api/config")
	if err != nil {
		return "", nil, fmt.Errorf("fetch config: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var cfg map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return "", nil, fmt.Errorf("fetch config: parse: %w", err)
	}

	gw := cfg["ghostwriter"]
	if gw != nil {
		if v, ok := gw["url"].(string); ok {
			gwURL = v
		}
		// Viper serialises []string as []interface{} in JSON.
		if raw, ok := gw["tokens"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok && s != "" {
					tokens = append(tokens, s)
				}
			}
		}
	}
	return gwURL, tokens, nil
}

// realPostTokens sends the updated token list to the daemon via POST /api/config.
func realPostTokens(tokens []string) error {
	payload := map[string]map[string]any{
		"ghostwriter": {"tokens": tokens},
	}
	resp, err := apiPost("/api/config", payload)
	if err != nil {
		return fmt.Errorf("post config: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		return fmt.Errorf("post config: daemon returned %s", resp.Status)
	}
	return nil
}

// realSelectTokenToDelete presents a huh selector for the --del flow.
// Returns ("", huh.ErrUserAborted) when the user selects Cancel or presses Escape/Ctrl+C.
func realSelectTokenToDelete(opts []tokenSelectOption) (string, error) {
	// Sentinel empty string for the explicit Cancel option.
	const cancelSentinel = ""

	options := make([]huh.Option[string], 0, len(opts)+1)
	options = append(options, huh.NewOption("Cancel", cancelSentinel))
	for _, o := range opts {
		options = append(options, huh.NewOption(o.label, o.token))
	}
	var chosen string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a service token to remove").
				Options(options...).
				Value(&chosen),
		),
	).WithTheme(grimTheme()).Run()
	if err != nil {
		return "", err
	}
	if chosen == cancelSentinel {
		return "", huh.ErrUserAborted
	}
	return chosen, nil
}
