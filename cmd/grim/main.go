// Package main is the entry point for the grim CLI.
//
// grim shell --project <id>   start an instrumented interactive shell
// grim daemon start           start the Ghostwriter sync daemon
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var cfgFile string

var rootCmd = &cobra.Command{
	Use:          "grim",
	Short:        "Grim — shell history capture and Ghostwriter sync",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return initConfig()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"path to config file (default: ~/.grim/config.yaml)")
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(tagCmd)
	rootCmd.AddCommand(serviceCmd)
	rootCmd.AddCommand(castCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(syncCmd)
}

// initConfig reads the config file and sets Viper defaults. Called by
// PersistentPreRunE so it runs before every subcommand.
func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".grim"))
			viper.AddConfigPath(filepath.Join(home, ".config", "grimoire"))
		}
		viper.AddConfigPath("config")
		viper.AddConfigPath(".")
	}

	viper.SetDefault("server.host", "127.0.0.1")
	viper.SetDefault("server.port", 7777)
	viper.SetDefault("api.max_short_id_retries", 5)
	if home, err := os.UserHomeDir(); err == nil {
		viper.SetDefault("db.path", filepath.Join(home, ".grim", "grimoire.db"))
		viper.SetDefault("auth.token_path", filepath.Join(home, ".grim", "token"))
	} else {
		viper.SetDefault("db.path", "grimoire.db")
		viper.SetDefault("auth.token_path", "token")
	}
	viper.SetDefault("sync.interval_seconds", 30)
	viper.SetDefault("ghostwriter.auto_sync", true)
	viper.SetDefault("shell", "")
	viper.SetDefault("log.dir", ".")
	viper.SetDefault("log.filename", "grimoire.log")
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.max_mb", 50)
	viper.SetDefault("log.max_backups", 5)
	viper.SetDefault("cast.dir", "")

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("config: %w", err)
		}
		// Missing config file is fine — all defaults apply.
	}
	return nil
}

// generatePaneID returns a cryptographically random 16-char hex string used as
// a pane's stable identity for the lifetime of the shell session.
func generatePaneID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate pane id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
