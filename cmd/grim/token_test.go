package main

import (
	"errors"
	"io"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// maskToken
// ---------------------------------------------------------------------------

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short token returned as-is", "gwst_abc", "gwst_abc"},
		{"exactly 12 chars returned as-is", "gwst_abc12de", "gwst_abc12de"},
		{"long token masked", "gwst_abcdef1234567890xyz", "gwst_abc...0xyz"},
		{"token of 13 chars masked", "gwst_abcdefghi", "gwst_abc...fghi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, maskToken(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// runAppendToken
// ---------------------------------------------------------------------------

func TestRunAppendToken(t *testing.T) {
	tests := []struct {
		name         string
		newToken     string
		existing     []string
		wantPosted   []string
		wantNoChange bool
		getErr       error
		postErr      error
		wantErr      bool
	}{
		{
			name:       "appends to empty list",
			newToken:   "gwst_new",
			existing:   []string{},
			wantPosted: []string{"gwst_new"},
		},
		{
			name:       "appends to existing list",
			newToken:   "gwst_new",
			existing:   []string{"gwst_abc"},
			wantPosted: []string{"gwst_abc", "gwst_new"},
		},
		{
			name:         "deduplicates existing token",
			newToken:     "gwst_abc",
			existing:     []string{"gwst_abc"},
			wantNoChange: true,
		},
		{
			name:     "getConfig error returns error",
			newToken: "gwst_new",
			getErr:   errors.New("daemon down"),
			wantErr:  true,
		},
		{
			name:     "postTokens error returns error",
			newToken: "gwst_new",
			existing: []string{},
			postErr:  errors.New("write failed"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posted []string
			deps := tokenAppendDeps{
				getConfig: func() (string, []string, error) {
					return "https://gw.example.com", tt.existing, tt.getErr
				},
				postTokens: func(tokens []string) error {
					posted = tokens
					return tt.postErr
				},
				stdout: io.Discard,
			}

			err := runAppendToken(tt.newToken, deps)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNoChange {
				assert.Nil(t, posted, "postTokens should not be called for a duplicate")
			} else {
				assert.Equal(t, tt.wantPosted, posted)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runDeleteToken
// ---------------------------------------------------------------------------

func TestRunDeleteToken(t *testing.T) {
	tok1 := "gwst_abc1234567890xyz"
	tok2 := "gwst_def0987654321uvw"

	oplogs1 := []oplogEntry{{ID: 42, Name: "BASILISK", ProjectID: 1}}
	oplogs2 := []oplogEntry{{ID: 17, Name: "HAVOC", ProjectID: 2}}

	tests := []struct {
		name        string
		tokens      []string
		discoverFn  func(_, tok string) ([]oplogEntry, error)
		selectValue string
		selectErr   error
		wantPosted  []string
		wantNoPost  bool
		getErr      error
		postErr     error
		wantErr     bool
	}{
		{
			name:    "no tokens configured returns error",
			tokens:  []string{},
			wantErr: true,
		},
		{
			name:   "delete first of two tokens",
			tokens: []string{tok1, tok2},
			discoverFn: func(_, tok string) ([]oplogEntry, error) {
				if tok == tok1 {
					return oplogs1, nil
				}
				return oplogs2, nil
			},
			selectValue: tok1,
			wantPosted:  []string{tok2},
		},
		{
			name:        "delete only token results in empty list",
			tokens:      []string{tok1},
			discoverFn:  func(_, _ string) ([]oplogEntry, error) { return oplogs1, nil },
			selectValue: tok1,
			wantPosted:  []string{},
		},
		{
			name:       "user aborts selection → no change",
			tokens:     []string{tok1, tok2},
			discoverFn: func(_, _ string) ([]oplogEntry, error) { return oplogs1, nil },
			selectErr:  huh.ErrUserAborted,
			wantNoPost: true,
		},
		{
			name:        "discovery failure shown as unavailable, token still selectable",
			tokens:      []string{tok1},
			discoverFn:  func(_, _ string) ([]oplogEntry, error) { return nil, errors.New("unreachable") },
			selectValue: tok1,
			wantPosted:  []string{},
		},
		{
			name:    "getConfig error returns error",
			getErr:  errors.New("daemon down"),
			wantErr: true,
		},
		{
			name:        "postTokens error returns error",
			tokens:      []string{tok1},
			discoverFn:  func(_, _ string) ([]oplogEntry, error) { return oplogs1, nil },
			selectValue: tok1,
			postErr:     errors.New("write failed"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posted []string
			deps := tokenDeleteDeps{
				getConfig: func() (string, []string, error) {
					return "https://gw.example.com", tt.tokens, tt.getErr
				},
				discoverFn: tt.discoverFn,
				selectFn: func(_ []tokenSelectOption) (string, error) {
					if tt.selectErr != nil {
						return "", tt.selectErr
					}
					return tt.selectValue, nil
				},
				postTokens: func(tokens []string) error {
					posted = tokens
					return tt.postErr
				},
				stderr: io.Discard,
			}

			err := runDeleteToken(deps)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNoPost {
				assert.Nil(t, posted, "postTokens should not be called when selection is aborted")
			} else {
				assert.Equal(t, tt.wantPosted, posted)
			}
		})
	}
}
