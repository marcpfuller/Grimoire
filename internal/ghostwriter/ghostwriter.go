// Package ghostwriter implements grimsync.Pusher by writing finalized commands
// as OplogEntry rows to a Ghostwriter instance via its Hasura GraphQL API.
//
// Authentication uses a Ghostwriter service token (opaque string, not a JWT)
// passed as "Authorization: Bearer <token>". The token must have the OPLOG_RW
// preset on the target oplog.
package ghostwriter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/marcpfuller/grimoire/internal/db"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
)

// ansiRe matches ANSI / VT100 escape sequences produced by interactive shells:
//   - CSI sequences:  ESC [ <params> <final-byte>   (colors, cursor movement, etc.)
//   - OSC sequences:  ESC ] <text> BEL|ST            (includes our OSC 133 markers)
//   - Two-char seqs:  ESC <letter>                    (e.g. ESC M reverse index)
var ansiRe = regexp.MustCompile(
	`\x1b(?:` +
		`\[[0-9;?]*[A-Za-z]` + // CSI: ESC [ … final
		`|\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC: ESC ] … BEL or ST
		`|[@-Z\\-_]` + // Fe two-char escapes
		`)`,
)

// StripANSI removes all ANSI escape sequences from s, returning clean text
// suitable for storage in Ghostwriter's output field. Exported for testing.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// Config holds the connection parameters for a Ghostwriter instance.
type Config struct {
	// URL is the Ghostwriter base URL, e.g. "https://ghostwriter.example.com".
	// PushCommand appends "/v1/graphql" to reach the Hasura endpoint directly.
	URL string
	// Token is the Ghostwriter service token (opaque, not a JWT). It must have
	// the OPLOG_RW preset on the target oplog. Passed as "Bearer <token>".
	Token string
	// OplogID overrides auto-discovery when non-zero. Leave zero in production;
	// the daemon calls Init to discover the oplog from the token's scope.
	// Useful in tests to skip the discovery round-trip.
	OplogID int
}

// Client implements grimsync.Pusher against a live Ghostwriter instance.
type Client struct {
	store           db.Store
	cfg             Config
	http            *http.Client
	logger          *slog.Logger
	resolvedOplogID int // set by New/NewWithHTTP (cfg.OplogID) or discovered by Init
}

// New returns a Client. Call Init on the returned client before pushing commands.
// An http.Client with a 30-second timeout is used by default; inject a custom
// one by calling NewWithHTTP.
func New(store db.Store, cfg Config, logger *slog.Logger) *Client {
	return NewWithHTTP(store, cfg, &http.Client{Timeout: 30 * time.Second}, logger)
}

// NewWithHTTP is like New but accepts a caller-supplied http.Client, useful
// for tests that inject an httptest.Server transport.
func NewWithHTTP(store db.Store, cfg Config, hc *http.Client, logger *slog.Logger) *Client {
	return &Client{
		store:           store,
		cfg:             cfg,
		http:            hc,
		logger:          logger,
		resolvedOplogID: cfg.OplogID, // pre-populate; Init is a no-op when nonzero
	}
}

// Init resolves the oplog ID by querying Ghostwriter for the single oplog
// accessible to the configured service token. The result is cached and reused
// by every subsequent PushCommand call.
//
// Init is a no-op if OplogID was set explicitly in Config (useful for tests).
// It returns *grimsync.AuthError for HTTP 401/403 or Hasura access-denied.
func (c *Client) Init(ctx context.Context) error {
	if c.resolvedOplogID != 0 {
		return nil // already set via Config override or a previous call
	}
	if c.cfg.URL == "" || c.cfg.Token == "" {
		return fmt.Errorf("ghostwriter: URL and Token must be set before calling Init")
	}

	body, err := json.Marshal(map[string]any{"query": discoverQuery})
	if err != nil {
		return fmt.Errorf("ghostwriter: marshal discover request: %w", err)
	}

	endpoint := strings.TrimRight(c.cfg.URL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ghostwriter: build discover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ghostwriter: discover http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("ghostwriter: read discover response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &grimsync.AuthError{Err: fmt.Errorf("HTTP %d during oplog discovery", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ghostwriter: discover: unexpected status %d: %.200s", resp.StatusCode, respBody)
	}

	var result struct {
		Data struct {
			Oplogs []struct {
				ID int `json:"id"`
			} `json:"oplog_oplog"`
		} `json:"data"`
		Errors []gqlError `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("ghostwriter: parse discover response: %w", err)
	}
	if len(result.Errors) > 0 {
		return result.Errors[0].asError()
	}

	switch len(result.Data.Oplogs) {
	case 0:
		return fmt.Errorf("ghostwriter: token has no accessible oplog; verify the OPLOG_RW preset is set")
	case 1:
		c.resolvedOplogID = result.Data.Oplogs[0].ID
		if c.logger != nil {
			c.logger.Info("ghostwriter: discovered oplog", "oplog_id", c.resolvedOplogID)
		}
		return nil
	default:
		return fmt.Errorf("ghostwriter: token grants access to %d oplogs; set oplog_id explicitly", len(result.Data.Oplogs))
	}
}

// PushCommand implements grimsync.Pusher. It loads the command's output chunks
// from the store, assembles them into a clean string, then inserts a new
// OplogEntry into Ghostwriter via GraphQL.
//
// Errors:
//   - *grimsync.AuthError for HTTP 401/403 or Hasura access-denied responses.
//   - Plain errors for network failures, unexpected status codes, or GraphQL
//     errors that do not indicate an auth problem. The syncer retries on the
//     next tick for plain errors.
func (c *Client) PushCommand(ctx context.Context, cmd *db.Command) error {
	if c.cfg.URL == "" || c.cfg.Token == "" {
		return fmt.Errorf("ghostwriter: URL and Token must be configured")
	}
	if c.resolvedOplogID == 0 {
		return fmt.Errorf("ghostwriter: oplog ID not resolved; call Init before pushing")
	}

	// --- Assemble output ---
	chunks, err := c.store.GetOutputChunks(ctx, cmd.ID)
	if err != nil {
		return fmt.Errorf("ghostwriter: load output chunks: %w", err)
	}
	output := assembleOutput(chunks)

	// --- Build GraphQL request ---
	vars := map[string]any{
		"oplog_id":         c.resolvedOplogID,
		"command":          cmd.Command,
		"output":           output,
		"start_date":       msToRFC3339(cmd.StartedAt),
		"entry_identifier": cmd.ShortID,
	}
	if cmd.EndedAt != nil {
		vars["end_date"] = msToRFC3339(*cmd.EndedAt)
	}

	body, err := json.Marshal(map[string]any{
		"query":     insertMutation,
		"variables": vars,
	})
	if err != nil {
		return fmt.Errorf("ghostwriter: marshal request: %w", err)
	}

	// --- POST to Hasura ---
	endpoint := strings.TrimRight(c.cfg.URL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ghostwriter: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ghostwriter: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return fmt.Errorf("ghostwriter: read response: %w", err)
	}

	// HTTP-level auth failures.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &grimsync.AuthError{Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ghostwriter: unexpected status %d: %.200s", resp.StatusCode, respBody)
	}

	// --- Parse GraphQL response ---
	var gqlResp struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("ghostwriter: parse response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return gqlResp.Errors[0].asError()
	}

	if c.logger != nil {
		c.logger.Debug("ghostwriter: pushed command",
			"short_id", cmd.ShortID,
			"command", cmd.Command,
		)
	}
	return nil
}

// gqlError represents a single entry in a Hasura GraphQL errors array.
type gqlError struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions"`
}

// asError converts a GraphQL error to a Go error, returning *grimsync.AuthError
// for Hasura access-denied codes.
func (e gqlError) asError() error {
	code, _ := e.Extensions["code"].(string)
	if code == "access-denied" || code == "invalid-jwt" {
		return &grimsync.AuthError{Err: fmt.Errorf("graphql: %s (code=%s)", e.Message, code)}
	}
	return fmt.Errorf("graphql: %s", e.Message)
}

// assembleOutput joins output chunks sorted by offset into a single plain-text
// string. Raw PTY bytes (including ANSI escape sequences) are stripped so the
// result matches what the operator saw on screen, without terminal markup.
func assembleOutput(chunks []*db.OutputChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	// Chunks are returned ordered by offset_ms from SQLite, but sort defensively.
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].OffsetMs < chunks[j].OffsetMs
	})
	var sb strings.Builder
	for _, ch := range chunks {
		sb.Write(ch.Data)
	}
	return StripANSI(sb.String())
}

// msToRFC3339 converts a Unix millisecond timestamp to an RFC3339 string
// (UTC, nanosecond precision) suitable for Hasura's timestamptz type.
func msToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// insertMutation is the Hasura GraphQL mutation used to create an OplogEntry.
// The service token must have the OPLOG_RW preset on the target oplog; Hasura
// enforces the oplog_id_id constraint via the create_oplogentry_oplog_id header
// claim injected by Ghostwriter's webhook.
// discoverQuery fetches all oplogs visible to the current service token.
// Hasura's row-level permissions ensure the token sees only its authorized oplog(s).
const discoverQuery = `{ oplog_oplog { id } }`

const insertMutation = `
mutation GrimoireInsertOplogEntry(
  $oplog_id:         Int!
  $command:          String!
  $output:           String
  $start_date:       timestamptz
  $end_date:         timestamptz
  $entry_identifier: String
) {
  insert_oplog_oplogentry_one(object: {
    oplog_id_id:       $oplog_id
    command:           $command
    output:            $output
    start_date:        $start_date
    end_date:          $end_date
    entry_identifier:  $entry_identifier
  }) {
    id
  }
}`
