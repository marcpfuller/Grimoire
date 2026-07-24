// Package main is the entry point for the grim CLI.
//
// grim shell --project <id>   start an instrumented interactive shell
// grim daemon start           start the Ghostwriter sync daemon (stub)
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/api"
	internalpty "github.com/marcpfuller/grimoire/internal/pty"
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
		"path to config file (default: config/config.yaml)")
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(daemonCmd)
}

// initConfig reads the config file and sets Viper defaults. Called by
// PersistentPreRunE so it runs before every subcommand.
func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("config")
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".grim"))
		}
		viper.AddConfigPath(".")
	}

	viper.SetDefault("server.host", "127.0.0.1")
	viper.SetDefault("server.port", 7777)
	viper.SetDefault("api.max_short_id_retries", 5)
	viper.SetDefault("db.path", "grimoire.db")
	viper.SetDefault("shell", "")
	viper.SetDefault("log.dir", ".")
	viper.SetDefault("log.filename", "grimoire.log")
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.max_mb", 50)
	viper.SetDefault("log.max_backups", 5)

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

// sessionRegistry is a thread-safe map of active pane sessions.
// It implements api.SessionRegistry.
type sessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*internalpty.Session
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{sessions: make(map[string]*internalpty.Session)}
}

// Register adds sess under paneID. Must be called before the HTTP server
// starts accepting requests for this pane.
func (r *sessionRegistry) Register(paneID string, sess *internalpty.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[paneID] = sess
}

// Lookup implements api.SessionRegistry.
func (r *sessionRegistry) Lookup(paneID string) (api.PtySession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[paneID]
	return s, ok
}

// Remove implements api.SessionRegistry.
func (r *sessionRegistry) Remove(paneID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, paneID)
}

// DeactivateAll calls Deactivate on every live session. Used during graceful
// shutdown so no session is left in Capturing state.
func (r *sessionRegistry) DeactivateAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sessions {
		s.Deactivate()
	}
}
