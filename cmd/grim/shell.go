package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/api"
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

	addr := fmt.Sprintf("%s:%d",
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

	// --- db ---
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.UpsertPane(ctx, paneID, projectID, time.Now().UnixMilli()); err != nil {
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

	// --- session + registry ---
	reg := newSessionRegistry()
	sess := internalpty.New(store, paneID, shellBin, log)
	reg.Register(paneID, sess)

	// --- HTTP API server ---
	srv := api.New(store, reg, viper.GetInt("api.max_short_id_retries"), log)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
		}
	}()

	// Fail fast if the HTTP server cannot bind (e.g. port already in use).
	// ListenAndServe returns a bind error almost immediately, so 100 ms is
	// enough to distinguish a startup failure from normal running.
	select {
	case err := <-httpErrCh:
		return fmt.Errorf("http server: %w", err)
	case <-time.After(100 * time.Millisecond):
	}

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

	log.Info("grim shell started",
		"pane_id", paneID,
		"project_id", projectID,
		"addr", addr,
		"shell", shellBin,
	)

	// Run blocks until the shell exits or ctx is cancelled.
	runErr := sess.Run(ctx)

	signal.Stop(sigCh)

	// --- cleanup (best-effort; use a fresh context) ---
	reg.DeactivateAll()
	_ = store.DeregisterPane(context.Background(), paneID)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)

	// Surface HTTP server startup errors (e.g. "address already in use") only
	// if the shell itself exited cleanly — a port conflict is the root cause.
	select {
	case httpErr := <-httpErrCh:
		if runErr == nil {
			return fmt.Errorf("http server: %w", httpErr)
		}
	default:
	}

	return runErr
}
