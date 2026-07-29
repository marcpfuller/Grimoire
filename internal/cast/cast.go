// Package cast synthesizes Asciinema v2 .cast files from recorded command
// sequences stored in a Grimoire database.
package cast

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/marcpfuller/grimoire/internal/db"
)

// maxGapMs is the maximum inter-event gap in the synthesized .cast file.
// Gaps longer than this are compressed to maxGapMs milliseconds so recordings
// do not have long dead periods.
const maxGapMs = 2000

// Store is the subset of db.Store required by Synthesize. Using a narrow
// interface keeps the cast package loosely coupled and easy to test.
type Store interface {
	GetCommandByShortID(ctx context.Context, shortID string) (*db.Command, error)
	GetOutputChunks(ctx context.Context, commandID int64) ([]*db.OutputChunk, error)
	GetPane(ctx context.Context, paneID string) (*db.Pane, error)
}

// Params holds the parameters for a cast synthesis run.
type Params struct {
	// ShortIDs is the ordered list of command short IDs to include in the cast.
	// IDs may be non-contiguous (commands between them are skipped) but must be
	// supplied in chronological (ascending seq) order and must all belong to the
	// same pane session. At least one ID is required.
	ShortIDs []string
	// OutDir is the directory used for the default timestamped filename.
	// Ignored when OutFile is non-empty.
	OutDir string
	// OutFile is the explicit output path. When empty, a timestamped filename
	// is generated inside OutDir.
	OutFile string
}

// castHeader is the Asciinema v2 header line.
type castHeader struct {
	Version   int    `json:"version"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title,omitempty"`
}

// Synthesize resolves cfg.ShortIDs from the store, validates ordering, writes
// an Asciinema v2 .cast file containing only the selected commands, and returns
// the path of the written file.
func Synthesize(ctx context.Context, store Store, cfg Params) (string, error) {
	if len(cfg.ShortIDs) == 0 {
		return "", fmt.Errorf("cast: at least one short ID is required")
	}

	// Resolve every requested ID.
	cmds := make([]*db.Command, len(cfg.ShortIDs))
	for i, id := range cfg.ShortIDs {
		cmd, err := store.GetCommandByShortID(ctx, id)
		if err != nil {
			return "", fmt.Errorf("cast: resolve ID %q: %w", id, err)
		}
		cmds[i] = cmd
	}

	// All commands must belong to the same pane (terminal dimensions are
	// per-pane and the timeline must be coherent).
	paneID := cmds[0].PaneID
	for _, cmd := range cmds[1:] {
		if cmd.PaneID != paneID {
			return "", fmt.Errorf("cast: command %q belongs to pane %q, expected %q — all IDs must be from the same pane", cmd.ShortID, cmd.PaneID, paneID)
		}
	}

	// Sort commands into chronological order and deduplicate by Seq so that
	// passing the same ID twice (or two IDs that map to the same command) is
	// handled gracefully.
	slices.SortFunc(cmds, func(a, b *db.Command) int {
		return int(a.Seq - b.Seq)
	})
	cmds = slices.CompactFunc(cmds, func(a, b *db.Command) bool {
		return a.Seq == b.Seq
	})

	// Fetch terminal dimensions from the pane.
	pane, err := store.GetPane(ctx, paneID)
	if err != nil {
		return "", fmt.Errorf("cast: get pane %q: %w", paneID, err)
	}

	// Determine output file path.
	outPath := cfg.OutFile
	if outPath == "" {
		ts := time.Now().Format("20060102-150405")
		outPath = filepath.Join(cfg.OutDir, fmt.Sprintf("grimoire-%s.cast", ts))
		// Avoid overwriting an existing file if two exports land in the same second.
		if _, err := os.Stat(outPath); err == nil {
			outPath = filepath.Join(cfg.OutDir, fmt.Sprintf("grimoire-%s-%d.cast", ts, time.Now().UnixNano()))
		}
	}

	// Ensure output directory exists.
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return "", fmt.Errorf("cast: create output dir: %w", err)
	}

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("cast: open output file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)

	// Write Asciinema v2 header.
	hdr := castHeader{
		Version:   2,
		Width:     pane.Width,
		Height:    pane.Height,
		Timestamp: cmds[0].StartedAt / 1000, // ms → s
	}
	if err := enc.Encode(hdr); err != nil {
		return "", fmt.Errorf("cast: write header: %w", err)
	}

	// Write events. All events are type "o" (output). Each command produces:
	//   1. A bare "cmd\r\n" line (the prompt + command text the user typed).
	//   2. One event per output chunk.
	// Timestamps are expressed as floating-point seconds from the start of the
	// recording and are compressed so no gap exceeds maxGapMs milliseconds.
	baseMs := cmds[0].StartedAt
	curMs := int64(0) // elapsed ms from baseMs, post-compression

	writeEvent := func(absMs int64, data []byte) error {
		rawMs := absMs - baseMs
		if rawMs < 0 {
			rawMs = 0
		}
		// Compress gap.
		gap := rawMs - curMs
		if gap > maxGapMs {
			gap = maxGapMs
		}
		if gap < 0 {
			gap = 0
		}
		curMs += gap
		secs := float64(curMs) / 1000.0
		event := [3]any{secs, "o", string(data)}
		return enc.Encode(event)
	}

	for _, cmd := range cmds {
		// Emit the command line as a bare event at the command's start time.
		cmdLine := []byte(cmd.Command + "\r\n")
		if err := writeEvent(cmd.StartedAt, cmdLine); err != nil {
			return "", fmt.Errorf("cast: write command event: %w", err)
		}

		chunks, err := store.GetOutputChunks(ctx, cmd.ID)
		if err != nil {
			return "", fmt.Errorf("cast: get chunks for command %d: %w", cmd.ID, err)
		}
		for _, chunk := range chunks {
			chunkMs := cmd.StartedAt + chunk.OffsetMs
			if err := writeEvent(chunkMs, chunk.Data); err != nil {
				return "", fmt.Errorf("cast: write chunk event: %w", err)
			}
		}
	}

	return outPath, nil
}
