package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func discardDeps(fetch func() ([]oplogEntry, error)) resolveOplogDeps {
	return resolveOplogDeps{fetch: fetch, stderr: io.Discard}
}

var singleOplog = []oplogEntry{{ID: 42, Name: "BASILISK Oplog", ProjectID: 7}}

// ---- numeric ID fast-path ----

func TestResolveOplog_NumericID_ReturnsImmediately(t *testing.T) {
	fetchCalled := false
	deps := discardDeps(func() ([]oplogEntry, error) {
		fetchCalled = true
		return nil, nil
	})
	id, name, err := resolveOplog("7", false, deps)
	require.NoError(t, err)
	assert.Equal(t, 7, id)
	assert.Empty(t, name)
	assert.False(t, fetchCalled, "fetch must not be called for a numeric ID")
}

func TestResolveOplog_NumericID_Zero_TreatedAsName(t *testing.T) {
	// "0" is not a valid oplog ID (must be > 0); treat as name lookup.
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return []oplogEntry{}, nil },
		stderr: io.Discard,
	}
	_, _, err := resolveOplog("0", false, deps)
	require.Error(t, err, "zero oplog arg with no matching name must fail")
}

func TestResolveOplog_NegativeNumeric_TreatedAsName(t *testing.T) {
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return []oplogEntry{}, nil },
		stderr: io.Discard,
	}
	_, _, err := resolveOplog("-1", false, deps)
	require.Error(t, err)
}

// ---- named oplog: fetch + match ----

func TestResolveOplog_NameExactMatch(t *testing.T) {
	deps := discardDeps(func() ([]oplogEntry, error) { return singleOplog, nil })
	id, name, err := resolveOplog("BASILISK Oplog", false, deps)
	require.NoError(t, err)
	assert.Equal(t, 42, id)
	assert.Equal(t, "BASILISK Oplog", name)
}

func TestResolveOplog_NameMatch_CaseInsensitive(t *testing.T) {
	deps := discardDeps(func() ([]oplogEntry, error) { return singleOplog, nil })
	id, name, err := resolveOplog("basilisk oplog", false, deps)
	require.NoError(t, err)
	assert.Equal(t, 42, id)
	assert.Equal(t, "BASILISK Oplog", name, "returned name must use the canonical casing from the server")
}

func TestResolveOplog_NameMatch_FirstWins(t *testing.T) {
	oplogs := []oplogEntry{
		{ID: 1, Name: "Alpha"},
		{ID: 2, Name: "alpha"}, // duplicate via case-insensitive match
	}
	deps := discardDeps(func() ([]oplogEntry, error) { return oplogs, nil })
	id, _, err := resolveOplog("ALPHA", false, deps)
	require.NoError(t, err)
	assert.Equal(t, 1, id, "first matching entry wins")
}

// ---- named oplog: no match ----

func TestResolveOplog_NameNoMatch_NoTTY_HardError(t *testing.T) {
	oplogs := []oplogEntry{{ID: 1, Name: "Alpha"}, {ID: 2, Name: "Beta"}}
	deps := discardDeps(func() ([]oplogEntry, error) { return oplogs, nil })
	_, _, err := resolveOplog("Gamma", false, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Gamma", "error must name the missing oplog")
	assert.Contains(t, err.Error(), "Alpha", "error must list available oplogs")
	assert.Contains(t, err.Error(), "Beta")
}

func TestResolveOplog_NameNoMatch_TTY_ConfirmYes_LocalCapture(t *testing.T) {
	deps := resolveOplogDeps{
		fetch:     func() ([]oplogEntry, error) { return singleOplog, nil },
		confirmFn: func(_ string) (bool, error) { return true, nil },
		stderr:    io.Discard,
	}
	id, name, err := resolveOplog("typo-name", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Equal(t, "typo-name", name, "original arg preserved as name for local capture")
}

func TestResolveOplog_NameNoMatch_TTY_ConfirmNo_HardError(t *testing.T) {
	deps := resolveOplogDeps{
		fetch:     func() ([]oplogEntry, error) { return singleOplog, nil },
		confirmFn: func(_ string) (bool, error) { return false, nil },
		stderr:    io.Discard,
	}
	_, _, err := resolveOplog("typo-name", true, deps)
	require.Error(t, err)
}

func TestResolveOplog_NameNoMatch_TTY_NilConfirmFn_HardError(t *testing.T) {
	// confirmFn nil means non-interactive path was taken — treat as no TTY.
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return singleOplog, nil },
		stderr: io.Discard,
	}
	_, _, err := resolveOplog("typo-name", true, deps)
	require.Error(t, err)
}

// ---- fetch error with a named arg ----

func TestResolveOplog_FetchError_WithName_HardError(t *testing.T) {
	deps := discardDeps(func() ([]oplogEntry, error) {
		return nil, errors.New("connection refused")
	})
	_, _, err := resolveOplog("BASILISK", false, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve oplog name")
	assert.Contains(t, err.Error(), "BASILISK")
}

// ---- empty arg ----

func TestResolveOplog_EmptyArg_NoTTY_LocalCapture(t *testing.T) {
	fetchCalled := false
	deps := discardDeps(func() ([]oplogEntry, error) {
		fetchCalled = true
		return nil, nil
	})
	id, name, err := resolveOplog("", false, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Empty(t, name)
	assert.False(t, fetchCalled, "fetch must not be called when arg is empty and not a TTY")
}

func TestResolveOplog_EmptyArg_TTY_EmptyOplogs_WarnAndLocalCapture(t *testing.T) {
	var warnBuf strings.Builder
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return []oplogEntry{}, nil },
		stderr: &warnBuf,
	}
	id, name, err := resolveOplog("", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Empty(t, name)
	assert.Contains(t, warnBuf.String(), "no oplogs available")
}

func TestResolveOplog_EmptyArg_TTY_FetchError_WarnAndLocalCapture(t *testing.T) {
	// Daemon may not be running — not an error when no oplog was requested.
	var warnBuf strings.Builder
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return nil, errors.New("connection refused") },
		stderr: &warnBuf,
	}
	id, name, err := resolveOplog("", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Empty(t, name)
	assert.Contains(t, warnBuf.String(), "proceeding with local capture")
}

func TestResolveOplog_EmptyArg_TTY_SelectOplog_ReturnsChosen(t *testing.T) {
	chosen := oplogEntry{ID: 5, Name: "Op Echo", ProjectID: 10}
	deps := resolveOplogDeps{
		fetch:    func() ([]oplogEntry, error) { return []oplogEntry{chosen}, nil },
		selectFn: func(_ []oplogEntry) (oplogEntry, error) { return chosen, nil },
		stderr:   io.Discard,
	}
	id, name, err := resolveOplog("", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 5, id)
	assert.Equal(t, "Op Echo", name)
}

func TestResolveOplog_EmptyArg_TTY_SelectAborted_LocalCapture(t *testing.T) {
	deps := resolveOplogDeps{
		fetch:    func() ([]oplogEntry, error) { return singleOplog, nil },
		selectFn: func(_ []oplogEntry) (oplogEntry, error) { return oplogEntry{}, errSelectionAborted },
		stderr:   io.Discard,
	}
	id, name, err := resolveOplog("", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Empty(t, name)
}

func TestResolveOplog_EmptyArg_TTY_NilSelectFn_LocalCapture(t *testing.T) {
	// selectFn nil means no interactive picker available — fall back to local capture.
	deps := resolveOplogDeps{
		fetch:  func() ([]oplogEntry, error) { return singleOplog, nil },
		stderr: io.Discard,
	}
	id, name, err := resolveOplog("", true, deps)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
	assert.Empty(t, name)
}
