package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/logger"
	internalpty "github.com/marcpfuller/grimoire/internal/pty"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start a Grimoire-instrumented interactive shell",
	RunE:  runShell,
}

func init() {
	shellCmd.Flags().String("project", "", "Ghostwriter project ID (required)")
	_ = shellCmd.MarkFlagRequired("project")
}

func runShell(cmd *cobra.Command, _ []string) error {
	projectID, _ := cmd.Flags().GetString("project")

	daemonAddr := fmt.Sprintf("http://%s:%d",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
	)

	// --- logger ---
	var level slog.Level
	_ = level.UnmarshalText([]byte(viper.GetString("log.level")))

	log, rotator, _, err := logger.New(logger.Config{
		Dir:        viper.GetString("log.dir"),
		Filename:   viper.GetString("log.filename"),
		Level:      level,
		MaxMB:      viper.GetInt("log.max_mb"),
		MaxBackups: viper.GetInt("log.max_backups"),
	})
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer rotator.Close() //nolint:errcheck

	// --- db (output chunk writes only) ---
	store, err := db.Open(viper.GetString("db.path"))
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer store.Close() //nolint:errcheck

	// --- pane ---
	paneID, err := generatePaneID()
	if err != nil {
		return err
	}

	// Register this pane with the daemon. Fails fast if the daemon is not
	// running — shell hooks require the daemon's HTTP API to be available.
	if err := registerPane(daemonAddr, paneID, projectID); err != nil {
		return fmt.Errorf("register pane: %w", err)
	}

	// --- shell binary ---
	shellBin := viper.GetString("shell")
	if shellBin == "" {
		shellBin = os.Getenv("SHELL")
	}
	if shellBin == "" {
		shellBin = "/bin/sh"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel ctx on SIGTERM / SIGINT so sess.Run returns cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	sess := internalpty.New(store, paneID, shellBin, log)

	log.Info("grim shell started",
		"pane_id", paneID,
		"project_id", projectID,
		"daemon", daemonAddr,
		"shell", shellBin,
	)

	// Run blocks until the shell exits or ctx is cancelled.
	runErr := sess.Run(ctx)

	signal.Stop(sigCh)

	// Deregister pane with daemon (best-effort; abnormal exits are fine).
	_ = deregisterPane(daemonAddr, paneID)

	return runErr
}

// registerPane calls POST /api/register on the daemon.
func registerPane(addr, paneID, projectID string) error {
	body, _ := json.Marshal(map[string]string{
		"pane_id":    paneID,
		"project_id": projectID,
	})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(addr+"/api/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon unreachable (%s): %w", addr, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("daemon returned %d for /api/register", resp.StatusCode)
	}
	return nil
}

// deregisterPane calls POST /api/deregister on the daemon.
func deregisterPane(addr, paneID string) error {
	body, _ := json.Marshal(map[string]string{"pane_id": paneID})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(addr+"/api/deregister", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("deregister: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("daemon returned %d for /api/deregister", resp.StatusCode)
	}
	return nil
}
