package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/marcpfuller/grimoire/internal/api"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Print the command history for a pane",
	Long: `Print the recorded command history for a pane session.

By default the current pane is used (GRIMOIRE_PANE_ID environment variable).
Pass --pane to target a different pane, or --all to show every pane.

Example:
  grim history
  grim history --pane pane-abc123
  grim history --all`,
	RunE: runHistory,
}

var (
	historyPaneID string
	historyAll    bool
)

func init() {
	historyCmd.Flags().StringVar(&historyPaneID, "pane", "",
		"pane ID to show history for (default: $GRIMOIRE_PANE_ID)")
	historyCmd.Flags().BoolVar(&historyAll, "all", false,
		"show history for all panes")
}

func runHistory(cmd *cobra.Command, _ []string) error {
	resp, err := apiGet("/api/sessions")
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != 200 {
		return fmt.Errorf("history: daemon returned %s", resp.Status)
	}

	var sessions []api.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return fmt.Errorf("history: decode response: %w", err)
	}

	// Determine which pane(s) to display.
	paneID := historyPaneID
	if paneID == "" && !historyAll {
		paneID = os.Getenv("GRIMOIRE_PANE_ID")
	}
	if paneID == "" && !historyAll {
		return fmt.Errorf("history: no pane ID — set $GRIMOIRE_PANE_ID, pass --pane, or use --all")
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tTIMESTAMP\tDURATION\tEXIT\tCOMMAND")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 7)+"\t"+strings.Repeat("-", 19)+"\t"+strings.Repeat("-", 8)+"\t"+strings.Repeat("-", 4)+"\t"+strings.Repeat("-", 7))

	printed := 0
	for _, s := range sessions {
		if !historyAll && s.ID != paneID {
			continue
		}
		if historyAll && len(sessions) > 1 {
			_, _ = fmt.Fprintf(w, "=== pane: %s ===\n", s.ID)
		}
		for _, c := range s.Commands {
			exit := "-"
			if c.ExitCode != nil {
				exit = fmt.Sprintf("%d", *c.ExitCode)
			}
			// Truncate timestamp to just the time portion for readability.
			ts := c.Timestamp
			if len(ts) >= 19 {
				ts = ts[:19]
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				c.ID, ts, c.Duration, exit, c.Command)
			printed++
		}
	}
	_ = w.Flush()

	if printed == 0 {
		if historyAll {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no commands recorded")
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "no commands recorded for pane %s\n", paneID)
		}
	}
	return nil
}
