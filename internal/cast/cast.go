// Package cast synthesizes Asciinema v2 .cast files from recorded command
// sequences stored in a Grimoire database.
package cast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	GetCommandsByPaneSeqRange(ctx context.Context, paneID string, firstSeq, lastSeq int64) ([]*db.Command, error)
}

// Params holds the parameters for a cast synthesis run.
type Params struct {
	// FirstShortID and LastShortID identify the inclusive command range.
	FirstShortID string
	LastShortID  string
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

// Synthesize queries the store for the command range identified by cfg,
// writes an Asciinema v2 .cast file, and returns the path of the written file.
func Synthesize(ctx context.Context, store Store, cfg Params) (string, error) {
	// Resolve first and last commands.
	first, err := store.GetCommandByShortID(ctx, cfg.FirstShortID)
	if err != nil {
		return "", fmt.Errorf("cast: resolve first ID %q: %w", cfg.FirstShortID, err)
	}
	last := first
	if cfg.LastShortID != cfg.FirstShortID {
		last, err = store.GetCommandByShortID(ctx, cfg.LastShortID)
		if err != nil {
			return "", fmt.Errorf("cast: resolve last ID %q: %w", cfg.LastShortID, err)
		}
	}

	// Both commands must belong to the same pane.
	if first.PaneID != last.PaneID {
		return "", errors.New("cast: commands belong to different panes")
	}
	paneID := first.PaneID

	// Seq ordering must be non-decreasing.
	if last.Seq < first.Seq {
		return "", fmt.Errorf("cast: last command seq %d is before first command seq %d", last.Seq, first.Seq)
	}

	// Fetch terminal dimensions from the pane.
	pane, err := store.GetPane(ctx, paneID)
	if err != nil {
		return "", fmt.Errorf("cast: get pane %q: %w", paneID, err)
	}

	// Fetch all commands in the range.
	cmds, err := store.GetCommandsByPaneSeqRange(ctx, paneID, first.Seq, last.Seq)
	if err != nil {
		return "", fmt.Errorf("cast: query command range: %w", err)
	}

	// Determine output file path.
	outPath := cfg.OutFile
	if outPath == "" {
		ts := time.Now().Unix()
		outPath = filepath.Join(cfg.OutDir, fmt.Sprintf("grimoire-%d.cast", ts))
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
		Timestamp: first.StartedAt / 1000, // ms → s
	}
	if err := enc.Encode(hdr); err != nil {
		return "", fmt.Errorf("cast: write header: %w", err)
	}

	// Write events. All events are type "o" (output). Each command produces:
	//   1. A bare "cmd\r\n" line (the prompt + command text the user typed).
	//   2. One event per output chunk.
	// Timestamps are expressed as floating-point seconds from the start of the
	// recording and are compressed so no gap exceeds maxGapMs milliseconds.
	baseMs := first.StartedAt
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
