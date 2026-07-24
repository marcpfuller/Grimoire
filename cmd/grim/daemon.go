package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the Grimoire background sync daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Ghostwriter sync daemon (not yet implemented)",
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("daemon start: not yet implemented")
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
}
