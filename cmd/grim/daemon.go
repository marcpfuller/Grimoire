package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/api"
	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/ghostwriter"
	"github.com/marcpfuller/grimoire/internal/logger"
	grimsync "github.com/marcpfuller/grimoire/internal/sync"
	"github.com/marcpfuller/grimoire/web"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the Grimoire background sync daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Ghostwriter sync daemon",
	RunE:  runDaemonStart,
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
}

func runDaemonStart(_ *cobra.Command, _ []string) error {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- HTTP API server + embedded UI ---
	srv := api.New(store, viper.GetInt("api.max_short_id_retries"), log)

	uiDist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return fmt.Errorf("ui embed: %w", err)
	}
	topMux := http.NewServeMux()
	topMux.Handle("/api/", srv.Handler())
	topMux.Handle("/", http.FileServerFS(uiDist))

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           topMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
		}
	}()

	// Fail fast if the server cannot bind (e.g. port already in use).
	select {
	case err := <-httpErrCh:
		return fmt.Errorf("http server: %w", err)
	case <-time.After(100 * time.Millisecond):
	}

	// --- Ghostwriter pusher ---
	// Use the real client when URL and token are configured. The oplog is
	// discovered automatically from the service token at startup via Init.
	// Falls back to a no-op stub so the daemon can run without Ghostwriter
	// credentials during development or testing.
	var pusher grimsync.Pusher
	gwURL := viper.GetString("ghostwriter.url")
	gwToken := viper.GetString("ghostwriter.token")
	if gwURL != "" && gwToken != "" {
		client := ghostwriter.New(store, gwURL, gwToken, log)
		if err := client.Init(ctx); err != nil {
			return fmt.Errorf("ghostwriter init: %w", err)
		}
		pusher = client
		log.Info("ghostwriter pusher enabled", "url", gwURL)
	} else {
		pusher = &noopPusher{}
		log.Warn("ghostwriter pusher disabled — set ghostwriter.url and ghostwriter.token to enable sync")
	}

	// --- sync loop ---
	interval := time.Duration(viper.GetInt("sync.interval_seconds")) * time.Second
	syncer := grimsync.New(store, pusher, grimsync.Config{Interval: interval}, log)
	var syncWg sync.WaitGroup
	syncWg.Add(1)
	go func() {
		defer syncWg.Done()
		syncer.Run(ctx)
	}()

	// --- signals ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	log.Info("grim daemon started",
		"addr", addr,
		"pid", os.Getpid(),
		"sync_interval", syncer.Interval(),
	)

	select {
	case <-sigCh:
		log.Info("grim daemon shutting down")
		cancel()
	case err := <-httpErrCh:
		return fmt.Errorf("http server: %w", err)
	}

	signal.Stop(sigCh)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)

	// Wait for the sync drain to finish (up to 10 s, enforced inside syncer.Run)
	// before store.Close() fires via defer.
	syncWg.Wait()

	return nil
}

// noopPusher is used when Ghostwriter credentials are not configured.
type noopPusher struct{}

func (n *noopPusher) PushCommand(_ context.Context, _ *db.Command) error { return nil }
