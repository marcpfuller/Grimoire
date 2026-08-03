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
or the web UI to find short IDs.

If the shell session was started without --project, use --project here to
supply a Ghostwriter project ID for this push. The ID is used once and is
not persisted to the database.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shortID := args[0]
		projectID, _ := cmd.Flags().GetString("project")

		body := map[string]string{"short_id": shortID}
		if projectID != "" {
			body["project_id"] = projectID
		}

		resp, err := apiPost("/api/sync", body)
		if err != nil {
			return fmt.Errorf("sync: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("sync: read response: %w", err)
		}

		if resp.StatusCode >= 400 {
			var errResp struct {
				Error string `json:"error"`
			}
			if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Error != "" {
				return fmt.Errorf("sync: %s", errResp.Error)
			}
			return fmt.Errorf("sync: daemon returned %s", resp.Status)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "synced %s to Ghostwriter\n", shortID)
		return nil
	},
}

func init() {
	syncCmd.Flags().String("project", "", "Ghostwriter project ID (one-time override; not persisted)")
}
