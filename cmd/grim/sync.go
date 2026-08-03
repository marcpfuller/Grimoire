package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync <short-id>",
	Short: "Push a single unsynced command to Ghostwriter immediately",
	Long: `Push a specific command to Ghostwriter by its short ID.

Use this command when ghostwriter.auto_sync is false and you want to
manually select which commands to send to the oplog.

The command must exist and must not already be synced. Use 'grim history'
or the web UI to find short IDs.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shortID := args[0]

		resp, err := apiPost("/api/sync", map[string]string{"short_id": shortID})
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("sync: read response: %w", err)
		}

		if resp.StatusCode >= 400 {
			var errResp struct {
				Error string `json:"error"`
			}
			if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error != "" {
				return fmt.Errorf("sync: %s", errResp.Error)
			}
			return fmt.Errorf("sync: daemon returned %s", resp.Status)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %s to Ghostwriter\n", shortID)
		return nil
	},
}
