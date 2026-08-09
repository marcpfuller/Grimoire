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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

func serviceEndpoint(rawURL, path string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("ghostwriter: invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ghostwriter: URL %q is missing scheme or host", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("ghostwriter: URL %q must use http or https", rawURL)
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// GraphqlEndpoint returns the /v1/graphql endpoint for the given raw URL.
// It strips any path, query, or fragment so that a URL like
// "https://gw.example.com/users/admin/" correctly becomes
// "https://gw.example.com/v1/graphql" rather than appending to a stale path.
// Exported for testing.
func GraphqlEndpoint(rawURL string) (string, error) {
	return serviceEndpoint(rawURL, "/v1/graphql")
}

// StripANSI removes all ANSI escape sequences from s, returning clean text
// suitable for storage in Ghostwriter's output field. Exported for testing.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// OplogEntry describes a single Ghostwriter oplog accessible to a service token.
type OplogEntry struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
}

// Client implements grimsync.Pusher against a live Ghostwriter instance.
type Client struct {
	store  db.Store
	url    string
	token  string
	http   *http.Client
	logger *slog.Logger
	oplogs []OplogEntry // populated by Init; nil until Init succeeds
}

// New returns a Client. Call Init on the returned client before pushing commands.
// An http.Client with a 30-second timeout is used by default; inject a custom
// one by calling NewWithHTTP.
func New(store db.Store, url, token string, logger *slog.Logger) *Client {
	return NewWithHTTP(store, url, token, &http.Client{Timeout: 30 * time.Second}, logger)
}

// NewWithHTTP is like New but accepts a caller-supplied http.Client, useful
// for tests that inject an httptest.Server transport.
func NewWithHTTP(store db.Store, url, token string, hc *http.Client, logger *slog.Logger) *Client {
	return &Client{store: store, url: url, token: token, http: hc, logger: logger}
}

// Init fetches all oplogs accessible to the configured token and caches them.
// The result is cached; subsequent calls are no-ops.
//
// Init must complete before any calls to PushCommand. Concurrent invocations
// of Init and PushCommand are not safe.
//
// It returns *grimsync.AuthError for HTTP 401/403 or Hasura access-denied.
func (c *Client) Init(ctx context.Context) error {
	if c.oplogs != nil {
		return nil // idempotent: already initialized
	}
	if c.url == "" || c.token == "" {
		return fmt.Errorf("ghostwriter: URL and Token must be set before calling Init")
	}

	body, err := json.Marshal(map[string]any{"query": initQuery})
	if err != nil {
		return fmt.Errorf("ghostwriter: marshal init request: %w", err)
	}

	endpoint, err := GraphqlEndpoint(c.url)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ghostwriter: build init request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ghostwriter: init http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("ghostwriter: read init response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &grimsync.AuthError{Err: fmt.Errorf("HTTP %d during Init", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ghostwriter: init: unexpected status %d: %.200s", resp.StatusCode, respBody)
	}

	var result struct {
		Data struct {
			Oplogs []struct {
				ID        int    `json:"id"`
				ProjectID int    `json:"projectId"`
				Name      string `json:"name"`
			} `json:"oplog"`
		} `json:"data"`
		Errors []gqlError `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("ghostwriter: parse init response: %w", err)
	}
	if len(result.Errors) > 0 {
		return result.Errors[0].asError()
	}
	if len(result.Data.Oplogs) == 0 {
		return fmt.Errorf("ghostwriter: token has no accessible oplog; verify the token has OPLOG_RW access to at least one project oplog")
	}

	oplogs := make([]OplogEntry, 0, len(result.Data.Oplogs))
	for _, o := range result.Data.Oplogs {
		oplogs = append(oplogs, OplogEntry{ID: o.ID, Name: o.Name, ProjectID: o.ProjectID})
	}
	c.oplogs = oplogs
	if c.logger != nil {
		c.logger.Info("ghostwriter: Init complete", "oplogs", len(oplogs))
	}
	return nil
}

// Oplogs returns the list of oplogs discovered during Init.
// Returns nil if Init has not been called.
func (c *Client) Oplogs() []OplogEntry {
	return c.oplogs
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
	if c.url == "" || c.token == "" {
		return fmt.Errorf("ghostwriter: URL and Token must be configured")
	}
	if c.oplogs == nil {
		return fmt.Errorf("ghostwriter: oplog list not initialized; call Init before pushing")
	}
	if cmd.OplogID == 0 {
		return fmt.Errorf("ghostwriter: command has no oplog ID; register pane with --oplog before pushing")
	}
	oplogID := cmd.OplogID

	// --- Assemble output ---
	chunks, err := c.store.GetOutputChunks(ctx, cmd.ID)
	if err != nil {
		return fmt.Errorf("ghostwriter: load output chunks: %w", err)
	}
	output := assembleOutput(chunks)
	metadata, err := c.store.GetCommandMetadata(ctx, cmd.ID)
	if errors.Is(err, db.ErrNotFound) {
		metadata = db.DefaultCommandMetadata(cmd)
	} else if err != nil {
		return fmt.Errorf("ghostwriter: load command metadata: %w", err)
	}

	// --- Build GraphQL request ---
	vars := map[string]any{
		"oplog_id":         oplogID,
		"command":          cmd.Command,
		"output":           output,
		"start_date":       msToRFC3339(cmd.StartedAt),
		"entry_identifier": cmd.ShortID,
		"operator_name":    metadata.Operator,
		"tool":             metadata.Tool,
		"user_context":     metadata.UserContext,
		"source_ip":        metadata.SourceIP,
		"dest_ip":          metadata.DestinationIP,
		"description":      metadata.Description,
		"comments":         metadata.Comments,
	}
	if cmd.EndedAt != nil {
		vars["end_date"] = msToRFC3339(*cmd.EndedAt)
	}

	mutation := insertMutation
	if cmd.SyncedGW {
		mutation = updateMutation
	}
	body, err := json.Marshal(map[string]any{
		"query":     mutation,
		"variables": vars,
	})
	if err != nil {
		return fmt.Errorf("ghostwriter: marshal request: %w", err)
	}

	// --- POST to Hasura ---
	endpoint, err := GraphqlEndpoint(c.url)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ghostwriter: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

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
	if cmd.SyncedGW {
		var updateResult struct {
			Update *struct {
				AffectedRows int `json:"affected_rows"`
			} `json:"update_oplogEntry"`
		}
		if err := json.Unmarshal(gqlResp.Data, &updateResult); err != nil {
			return fmt.Errorf("ghostwriter: parse update response: %w", err)
		}
		if updateResult.Update == nil || updateResult.Update.AffectedRows != 1 {
			affected := 0
			if updateResult.Update != nil {
				affected = updateResult.Update.AffectedRows
			}
			return fmt.Errorf("ghostwriter: update matched %d entries for command %q", affected, cmd.ShortID)
		}
	}

	if c.logger != nil {
		c.logger.Debug("ghostwriter: pushed command", "short_id", cmd.ShortID)
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

// initQuery maps every accessible oplog to its parent project. Uses the raw
// projectId FK column (Hasura custom name for project_id) rather than the
// project relationship, because the service role has column access but not
// relationship-traversal access to rolodex_project.
const initQuery = `{ oplog { id projectId name } }`

// insertMutation is the Hasura GraphQL mutation used to create an OplogEntry.
// The service token must have the OPLOG_RW preset on the target oplog; Hasura
// enforces the oplog_id_id constraint via the create_oplogentry_oplog_id header
// claim injected by Ghostwriter's webhook.
const insertMutation = `
mutation GrimoireInsertOplogEntry(
  $oplog_id:         bigint!
  $command:          String!
  $output:           String
  $start_date:       timestamptz
  $end_date:         timestamptz
  $entry_identifier: String
  $operator_name:    String
  $tool:             String
  $user_context:     String
  $source_ip:        String
  $dest_ip:          String
  $description:      String
  $comments:         String
) {
  insert_oplogEntry_one(object: {
    oplog:             $oplog_id
    command:           $command
    output:            $output
    startDate:         $start_date
    endDate:           $end_date
    entryIdentifier:   $entry_identifier
    operatorName:      $operator_name
    tool:              $tool
    userContext:       $user_context
    sourceIp:          $source_ip
    destIp:            $dest_ip
    description:       $description
    comments:          $comments
  }) {
    id
  }
}`

// updateMutation refreshes the Grimoire-owned fields on an existing entry.
// Oplog, entryIdentifier, and startDate form the integration-owned match. The
// service token constrains the oplog; including the original millisecond start
// time prevents one Grimoire instance from updating another instance's row if
// their short identifiers happen to collide.
const updateMutation = `
mutation GrimoireUpdateOplogEntry(
  $oplog_id:         bigint!
  $command:          String!
  $output:           String
  $start_date:       timestamptz
  $end_date:         timestamptz
  $entry_identifier: String!
  $operator_name:    String
  $tool:             String
  $user_context:     String
  $source_ip:        String
  $dest_ip:          String
  $description:      String
  $comments:         String
) {
  update_oplogEntry(
    where: {
      oplog: {_eq: $oplog_id}
      entryIdentifier: {_eq: $entry_identifier}
      startDate: {_eq: $start_date}
    }
    _set: {
      command:      $command
      output:       $output
      startDate:    $start_date
      endDate:      $end_date
      operatorName: $operator_name
      tool:         $tool
      userContext:  $user_context
      sourceIp:     $source_ip
      destIp:       $dest_ip
      description:  $description
      comments:     $comments
    }
  ) {
    affected_rows
  }
}`
