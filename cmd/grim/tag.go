package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var tagCmd = &cobra.Command{
	Use:   "tag --id <short-id> <label> [label...]",
	Short: "Attach one or more labels to a recorded command",
	Long: `Attach one or more labels to a command identified by its short ID.

The daemon must be running. Each label is posted individually; tagging stops
at the first error. Duplicate labels are rejected with a non-zero exit.

Example:
  grim tag --id a1b2c3d "Dumped hashes"
  grim tag --id a1b2c3d "Credential Access" "TA0006"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTag,
}

var tagID string

func init() {
	tagCmd.Flags().StringVar(&tagID, "id", "", "short ID of the command to tag (required)")
	_ = tagCmd.MarkFlagRequired("id")
}

func runTag(cmd *cobra.Command, labels []string) error {
	addr := fmt.Sprintf("http://%s:%d/api/tag",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
	)

	for _, label := range labels {
		if err := postTag(addr, tagID, label); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tagged %s: %q\n", tagID, label)
	}
	return nil
}

func postTag(addr, shortID, label string) error {
	body, err := json.Marshal(map[string]string{
		"short_id": shortID,
		"label":    label,
	})
	if err != nil {
		return fmt.Errorf("tag: marshal request: %w", err)
	}

	resp, err := http.Post(addr, "application/json", bytes.NewReader(body)) //nolint:gosec // G107: addr is localhost from config, not user input
	if err != nil {
		return fmt.Errorf("tag: daemon unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("tag: command %q not found", shortID)
	case http.StatusConflict:
		return fmt.Errorf("tag: %q already tagged with %q", shortID, label)
	default:
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tag: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
}
