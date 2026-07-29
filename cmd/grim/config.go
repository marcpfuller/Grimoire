package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or update daemon configuration at runtime",
}

var configGetCmd = &cobra.Command{
	Use:   "get [group.key]",
	Short: "Print one config key or all config",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfigGet,
}

var configSetCmd = &cobra.Command{
	Use:   "set <group.key> <value>",
	Short: "Update a config key on the running daemon",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

func init() {
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}

func runConfigGet(_ *cobra.Command, args []string) error {
	resp, err := apiGet("/api/config")
	if err != nil {
		return fmt.Errorf("config get: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("config get: read response: %w", err)
	}

	var cfg map[string]map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		return fmt.Errorf("config get: parse response: %w", err)
	}

	// Single key query.
	if len(args) == 1 {
		parts := strings.SplitN(args[0], ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("config get: key must be in group.key format, got %q", args[0])
		}
		group, key := parts[0], parts[1]
		g, ok := cfg[group]
		if !ok {
			return fmt.Errorf("config get: unknown group %q", group)
		}
		val, ok := g[key]
		if !ok {
			return fmt.Errorf("config get: unknown key %q in group %q", key, group)
		}
		_, _ = fmt.Println(val)
		return nil
	}

	// Full config table.
	w := tabwriter.NewWriter(configCmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KEY\tVALUE")

	groups := make([]string, 0, len(cfg))
	for g := range cfg {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	for _, group := range groups {
		keys := make([]string, 0, len(cfg[group]))
		for k := range cfg[group] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := cfg[group][key]
			display := fmt.Sprint(val)
			if strings.Contains(strings.ToLower(key), "token") ||
				strings.Contains(strings.ToLower(key), "secret") ||
				strings.Contains(strings.ToLower(key), "password") {
				if display != "" && display != "<nil>" {
					display = "********"
				}
			}
			_, _ = fmt.Fprintf(w, "%s.%s\t%s\n", group, key, display)
		}
	}
	_ = w.Flush()
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	dotKey, value := args[0], args[1]

	parts := strings.SplitN(dotKey, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("config set: key must be in group.key format, got %q", dotKey)
	}
	group, key := parts[0], parts[1]

	payload := map[string]map[string]any{
		group: {key: value},
	}

	resp, err := apiPost("/api/config", payload)
	if err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("config set: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("config set: daemon error: %s", errResp.Error)
		}
		return fmt.Errorf("config set: daemon returned %s", resp.Status)
	}

	var result struct {
		Changed []string `json:"changed"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("config set: parse response: %w", err)
	}

	if len(result.Changed) == 0 {
		_, _ = fmt.Fprintln(configCmd.OutOrStdout(), "no change (value was already set)")
	} else {
		_, _ = fmt.Fprintf(configCmd.OutOrStdout(), "updated: %s\n", strings.Join(result.Changed, ", "))
	}
	return nil
}
