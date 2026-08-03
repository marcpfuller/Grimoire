package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/auth"
	"github.com/marcpfuller/grimoire/internal/db"
	"github.com/marcpfuller/grimoire/internal/logger"
	internalpty "github.com/marcpfuller/grimoire/internal/pty"
)

//go:embed hooks/grimoire.zsh
var zshHook []byte

//go:embed hooks/grimoire.bash
var bashHook []byte

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start a Grimoire-instrumented interactive shell",
	RunE:  runShell,
}

func init() {
	shellCmd.Flags().String("project", "", "Ghostwriter project ID or codename (optional; commands captured locally when omitted)")
	_ = shellCmd.RegisterFlagCompletionFunc("project", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		// Suppress file completion — project values are free-form IDs or codenames.
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
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

	// Load the bearer token written by the daemon on first start.
	token, err := auth.Load(viper.GetString("auth.token_path"))
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Register this pane with the daemon. Fails fast if the daemon is not
	// running — shell hooks require the daemon's HTTP API to be available.
	width, height, sizeErr := term.GetSize(int(os.Stdin.Fd()))
	if sizeErr != nil {
		// Fall back to sensible defaults when stdin is not a terminal
		// (e.g. testing or piped input).
		width, height = 220, 50
	}
	if err := registerPane(daemonAddr, paneID, projectID, token, width, height); err != nil {
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

	// --- shell hook auto-injection ---
	// Build shell-specific args/env so the Grimoire hooks are sourced
	// automatically without requiring operators to modify their rc files.
	grimExe, _ := os.Executable()
	shellArgs, extraEnv, hookCleanup, err := buildShellInit(shellBin, cmd.Root(), grimExe)
	if err != nil {
		log.Warn("grim shell: hook injection failed, continuing without capture hooks",
			"shell", shellBin, "err", err)
		hookCleanup = func() {}
	}
	defer hookCleanup()

	// Pass the resolved token path to the child shell so hooks read from the
	// same location as the daemon, regardless of the auth.token_path config.
	// The path is not sensitive — only the 0600 token file itself is.
	if resolvedPath, pathErr := auth.ResolvePath(viper.GetString("auth.token_path")); pathErr == nil {
		extraEnv = append(extraEnv, "GRIMOIRE_TOKEN_PATH="+resolvedPath)
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

	// Warn the operator on stderr if the daemon becomes unreachable mid-session.
	go watchDaemon(ctx, daemonAddr, token)

	sess := internalpty.New(store, internalpty.SessionConfig{
		PaneID:     paneID,
		Shell:      shellBin,
		DaemonAddr: daemonAddr,
		ShellArgs:  shellArgs,
		ExtraEnv:   extraEnv,
	}, log)

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
	_ = deregisterPane(daemonAddr, paneID, token)

	return runErr
}

// buildShellInit creates temporary init files for the detected shell type and
// returns the extra args and env vars needed to auto-source the Grimoire hooks.
// cleanup must be called after the shell exits to remove temp files.
//
//   - zsh:  sets ZDOTDIR to a temp dir whose .zshrc sources the real ~/.zshrc
//     then the Grimoire zsh hook. No args needed.
//   - bash: returns --init-file pointing to a temp file that sources ~/.bashrc
//     then the Grimoire bash hook.
//   - other shells: returns no-ops; operators must source hooks manually.
//
// root is the root cobra.Command; its completion script is appended to the
// init content so Tab-completion for grim subcommands works inside the shell.
// grimExe is the absolute path of the running binary; an extra completion
// binding is registered for it so Tab works when invoked as /tmp/grim or a
// full path, not just the bare `grim` word.
func buildShellInit(shellBin string, root *cobra.Command, grimExe string) (shellArgs, extraEnv []string, cleanup func(), err error) {
	base := strings.ToLower(filepath.Base(shellBin))
	noop := func() {}

	switch {
	case strings.HasSuffix(base, "zsh"):
		zdotdir, e := os.MkdirTemp("", "grimoire-zdotdir-*")
		if e != nil {
			return nil, nil, noop, fmt.Errorf("zsh init: mkdirtemp: %w", e)
		}
		cleanup = func() { _ = os.RemoveAll(zdotdir) }

		// .zshrc: unset ZDOTDIR first so nested shells use the real one,
		// then source the operator's real .zshrc, then inject Grimoire hooks,
		// then source the grim completion script.
		var zshCompletion bytes.Buffer
		if root != nil {
			_ = root.GenZshCompletion(&zshCompletion)
		}
		// Bind completion to the binary's full path in addition to the bare
		// command name so `Tab` works when the user invokes /path/to/grim.
		var zshExtraBinding string
		if grimExe != "" {
			zshExtraBinding = fmt.Sprintf("compdef _grim %q\n", grimExe)
		}
		zshrc := fmt.Sprintf(
			"unset ZDOTDIR\n"+
				"[[ -f \"${HOME}/.zshrc\" ]] && source \"${HOME}/.zshrc\"\n"+
				"# --- Grimoire hooks (auto-injected by grim shell) ---\n"+
				"%s\n"+
				"# --- grim tab-completion ---\n"+
				"autoload -Uz compinit && compinit -u 2>/dev/null\n"+
				"%s\n"+
				"%s",
			zshHook,
			zshCompletion.String(),
			zshExtraBinding,
		)
		if e := os.WriteFile(filepath.Join(zdotdir, ".zshrc"), []byte(zshrc), 0o600); e != nil {
			cleanup()
			return nil, nil, noop, fmt.Errorf("zsh init: write .zshrc: %w", e)
		}
		return nil, []string{"ZDOTDIR=" + zdotdir}, cleanup, nil

	case strings.HasSuffix(base, "bash"):
		f, e := os.CreateTemp("", "grimoire-bashrc-*")
		if e != nil {
			return nil, nil, noop, fmt.Errorf("bash init: createtemp: %w", e)
		}
		cleanup = func() { _ = os.Remove(f.Name()) }

		// init file: source the operator's real .bashrc, then Grimoire hooks,
		// then source the grim completion script.
		var bashCompletion bytes.Buffer
		if root != nil {
			_ = root.GenBashCompletionV2(&bashCompletion, false)
		}
		// Bind completion to the binary's full path in addition to the bare
		// command name so `Tab` works when the user invokes /path/to/grim.
		var bashExtraBinding string
		if grimExe != "" {
			bashExtraBinding = fmt.Sprintf("complete -o default -F __start_grim %q\n", grimExe)
		}
		content := fmt.Sprintf(
			"[[ -f \"${HOME}/.bashrc\" ]] && source \"${HOME}/.bashrc\"\n"+
				"# --- Grimoire hooks (auto-injected by grim shell) ---\n"+
				"%s\n"+
				"# --- grim tab-completion ---\n"+
				"%s\n"+
				"%s",
			bashHook,
			bashCompletion.String(),
			bashExtraBinding,
		)
		if _, e := f.WriteString(content); e != nil {
			_ = f.Close()
			cleanup()
			return nil, nil, noop, fmt.Errorf("bash init: write init file: %w", e)
		}
		_ = f.Close()
		return []string{"--init-file", f.Name()}, nil, cleanup, nil

	default:
		// sh, fish, etc. — no auto-source support; hooks must be sourced manually.
		return nil, nil, noop, nil
	}
}

// watchDaemon polls GET /api/health every 30s and writes a warning to stderr
// the first time the daemon becomes unreachable, then an info line when it
// recovers. It returns when ctx is cancelled (i.e. the shell exits).
func watchDaemon(ctx context.Context, addr, token string) {
	const interval = 30 * time.Second
	const timeout = 2 * time.Second

	client := &http.Client{Timeout: timeout}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	wasDown := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/api/health", nil)
			if err != nil {
				continue
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					resp.Body.Close() //nolint:errcheck,gosec // G104: best-effort drain
				}
				if !wasDown {
					wasDown = true
					fmt.Fprintln(os.Stderr,
						"\n\033[33m[grimoire] WARNING: daemon unreachable — commands are not being captured\033[0m")
				}
			} else {
				resp.Body.Close() //nolint:errcheck,gosec // G104: best-effort drain
				if wasDown {
					wasDown = false
					fmt.Fprintln(os.Stderr,
						"\033[32m[grimoire] daemon reconnected — capture resumed\033[0m")
				}
			}
		}
	}
}

// registerPane calls POST /api/register on the daemon.
func registerPane(addr, paneID, projectID, token string, width, height int) error {
	body, _ := json.Marshal(map[string]any{
		"pane_id":    paneID,
		"project_id": projectID,
		"width":      width,
		"height":     height,
	})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, addr+"/api/register", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("register pane: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
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
func deregisterPane(addr, paneID, token string) error {
	body, _ := json.Marshal(map[string]string{"pane_id": paneID})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, addr+"/api/deregister", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("deregister: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("deregister: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("daemon returned %d for /api/deregister", resp.StatusCode)
	}
	return nil
}
