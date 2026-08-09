package ghostwriter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcpfuller/grimoire/internal/db"
)

// MultiClient multiplexes PushCommand across multiple Ghostwriter service tokens.
// Each token owns a disjoint set of oplog IDs; routing is O(1) via a map built
// during Init. Use NewMultiClient to construct; call Init before pushing.
type MultiClient struct {
	store     db.Store
	url       string
	tokens    []string
	http      *http.Client
	logger    *slog.Logger
	clients   []*Client
	byOplog   map[int]*Client // oplogID → owning client, built by Init
	allOplogs []OplogEntry    // aggregated across all Init'd clients
}

// NewMultiClient returns a MultiClient for the given URL and token list.
// hc may be nil — a 30 s timeout client is used in that case.
func NewMultiClient(store db.Store, url string, tokens []string, hc *http.Client, logger *slog.Logger) *MultiClient {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &MultiClient{
		store:  store,
		url:    url,
		tokens: tokens,
		http:   hc,
		logger: logger,
	}
}

// Init initialises every client in parallel-sequential order (sequential for
// simplicity; the number of tokens is small in practice). Clients that fail
// Init are skipped with a warning — partial success is valid. Returns an error
// only if ALL clients fail or no tokens are configured.
func (m *MultiClient) Init(ctx context.Context) error {
	if m.byOplog != nil {
		return nil // idempotent
	}
	if len(m.tokens) == 0 {
		return fmt.Errorf("ghostwriter: no tokens configured")
	}

	byOplog := make(map[int]*Client)
	var allOplogs []OplogEntry
	clients := make([]*Client, 0, len(m.tokens))

	for _, tok := range m.tokens {
		c := NewWithHTTP(m.store, m.url, tok, m.http, m.logger)
		if err := c.Init(ctx); err != nil {
			if m.logger != nil {
				m.logger.Warn("ghostwriter: skipping token — Init failed", "err", err)
			}
			continue
		}
		clients = append(clients, c)
		for _, o := range c.Oplogs() {
			if _, exists := byOplog[o.ID]; !exists {
				// First-wins: if two tokens claim the same oplog ID, the first token handles it.
				byOplog[o.ID] = c
				allOplogs = append(allOplogs, o)
			}
		}
	}

	if len(clients) == 0 {
		return fmt.Errorf("ghostwriter: all tokens failed Init; sync unavailable")
	}

	m.clients = clients
	m.byOplog = byOplog
	m.allOplogs = allOplogs

	if m.logger != nil {
		m.logger.Info("ghostwriter: MultiClient Init complete",
			"tokens_ok", len(clients),
			"tokens_total", len(m.tokens),
			"oplogs", len(allOplogs),
		)
	}
	return nil
}

// PushCommand implements grimsync.Pusher. Routes cmd to the client that owns
// cmd.OplogID. Returns a plain error (not *AuthError) when no client owns the
// oplog — the syncer will retry on the next tick.
func (m *MultiClient) PushCommand(ctx context.Context, cmd *db.Command) error {
	if m.byOplog == nil {
		return fmt.Errorf("ghostwriter: MultiClient not initialized; call Init before pushing")
	}
	if cmd.OplogID == 0 {
		return fmt.Errorf("ghostwriter: command has no oplog ID; register pane with --oplog before pushing")
	}
	c, ok := m.byOplog[cmd.OplogID]
	if !ok {
		return fmt.Errorf("ghostwriter: no token owns oplog %d; check ghostwriter.tokens in config", cmd.OplogID)
	}
	return c.PushCommand(ctx, cmd)
}

// Oplogs returns the aggregated list of oplogs discovered across all Init'd clients.
// Returns nil if Init has not been called.
func (m *MultiClient) Oplogs() []OplogEntry {
	return m.allOplogs
}

// CheckHealth verifies Ghostwriter's database/cache status and confirms that at
// least one configured service token can access an oplog through Hasura GraphQL.
// Tokens are sent only to the normalized /v1/graphql endpoint.
func (m *MultiClient) CheckHealth(ctx context.Context) error {
	if m.url == "" || len(m.tokens) == 0 {
		return fmt.Errorf("ghostwriter: URL and at least one token must be configured")
	}

	statusEndpoint, err := serviceEndpoint(m.url, "/status/simple/")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusEndpoint, nil)
	if err != nil {
		return fmt.Errorf("ghostwriter: build status request: %w", err)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("ghostwriter: status request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ghostwriter: status returned HTTP %d", resp.StatusCode)
	}

	var lastErr error
	for _, token := range m.tokens {
		client := NewWithHTTP(m.store, m.url, token, m.http, nil)
		err := client.Init(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("ghostwriter: no configured token passed the authenticated health check: %w", lastErr)
}
