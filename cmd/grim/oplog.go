package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
)

// oplogEntry mirrors the JSON shape returned by GET /api/oplogs.
type oplogEntry struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
}

// errSelectionAborted is returned by selectFn (or the real huh selector) when
// the user dismisses the picker without choosing an entry. resolveOplog treats
// this as "proceed with local capture" — not an error.
var errSelectionAborted = huh.ErrUserAborted

// resolveOplogDeps holds injectable dependencies for resolveOplog.
// All fields are optional; only the fields reached by the given inputs are called.
type resolveOplogDeps struct {
	// fetch retrieves the oplog list from the daemon. Called when oplogArg is
	// non-numeric or is empty + isTTY.
	fetch func() ([]oplogEntry, error)

	// selectFn presents an interactive picker. Called only when oplogArg is
	// empty, isTTY is true, and fetch returns a non-empty list.
	// Return errSelectionAborted (or huh.ErrUserAborted) to proceed with local capture.
	selectFn func(oplogs []oplogEntry) (oplogEntry, error)

	// confirmFn presents a yes/no prompt. Called when oplogArg is a non-empty
	// name that does not match any fetched oplog and isTTY is true.
	// Return true to proceed with local capture.
	confirmFn func(msg string) (bool, error)

	// stderr receives warning messages. Defaults to os.Stderr when nil.
	stderr io.Writer
}

func (d *resolveOplogDeps) warn(format string, a ...any) {
	w := d.stderr
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, "grim: warning: "+format+"\n", a...)
}

// resolveOplog converts the --oplog flag value into a (oplogID, oplogName) pair.
//
//   - Numeric string > 0: returned immediately; no network call.
//   - Non-empty non-numeric string: calls fetch, case-insensitive name match.
//     On no match + TTY: calls confirmFn; on yes → local capture with original arg as name.
//   - Empty + !isTTY: local capture (0, "", nil); no network call.
//   - Empty + isTTY: calls fetch; shows selector when oplogs available.
func resolveOplog(oplogArg string, isTTY bool, deps resolveOplogDeps) (id int, name string, err error) {
	// --- numeric fast-path (must be positive) ---
	if n, parseErr := strconv.Atoi(oplogArg); parseErr == nil && n > 0 {
		return n, "", nil
	}

	// --- no oplog requested ---
	if oplogArg == "" {
		if !isTTY {
			return 0, "", nil // local capture
		}
		// Interactive: attempt to show selector
		oplogs, fetchErr := deps.fetch()
		if fetchErr != nil {
			deps.warn("could not fetch oplogs from daemon (%v); proceeding with local capture", fetchErr)
			return 0, "", nil
		}
		if len(oplogs) == 0 {
			deps.warn("no oplogs available; proceeding with local capture")
			return 0, "", nil
		}
		if deps.selectFn == nil {
			return 0, "", nil
		}
		chosen, selErr := deps.selectFn(oplogs)
		if selErr != nil {
			if errors.Is(selErr, errSelectionAborted) {
				return 0, "", nil
			}
			return 0, "", fmt.Errorf("oplog selector: %w", selErr)
		}
		return chosen.ID, chosen.Name, nil
	}

	// --- named oplog: resolve via fetch ---
	oplogs, fetchErr := deps.fetch()
	if fetchErr != nil {
		return 0, "", fmt.Errorf("cannot resolve oplog name %q: %w", oplogArg, fetchErr)
	}

	argLower := strings.ToLower(oplogArg)
	for _, o := range oplogs {
		if strings.ToLower(o.Name) == argLower {
			return o.ID, o.Name, nil
		}
	}

	// No match — build human-readable available list
	names := make([]string, len(oplogs))
	for i, o := range oplogs {
		names[i] = o.Name
	}
	available := strings.Join(names, ", ")
	if available == "" {
		available = "(none)"
	}

	if isTTY && deps.confirmFn != nil {
		msg := fmt.Sprintf("no oplog matching %q (available: %s); continue with local capture?", oplogArg, available)
		ok, confirmErr := deps.confirmFn(msg)
		if confirmErr != nil {
			return 0, "", fmt.Errorf("oplog confirm: %w", confirmErr)
		}
		if ok {
			return 0, oplogArg, nil // local capture, preserve original arg as name
		}
	}

	return 0, "", fmt.Errorf("no oplog matching %q; available: %s", oplogArg, available)
}

// fetchOplogs calls GET /api/oplogs on the daemon and returns the oplog list.
func fetchOplogs() ([]oplogEntry, error) {
	resp, err := apiGet("/api/oplogs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned %s", resp.Status)
	}
	var entries []oplogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode oplogs response: %w", err)
	}
	return entries, nil
}

// selectOplog presents an interactive huh.Select for the operator to pick an oplog.
// Returns huh.ErrUserAborted when the user presses Escape or Ctrl+C.
func selectOplog(oplogs []oplogEntry) (oplogEntry, error) {
	options := make([]huh.Option[oplogEntry], len(oplogs))
	for i, o := range oplogs {
		options[i] = huh.NewOption(o.Name, o)
	}
	var chosen oplogEntry
	err := huh.NewSelect[oplogEntry]().
		Title("Select Ghostwriter oplog").
		Options(options...).
		Value(&chosen).
		Run()
	return chosen, err
}

// confirmLocalCapture shows a huh.Confirm asking whether to proceed with local capture.
func confirmLocalCapture(msg string) (bool, error) {
	var ok bool
	err := huh.NewConfirm().
		Title(msg).
		Value(&ok).
		Run()
	return ok, err
}
