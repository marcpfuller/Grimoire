package main

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalRootCmd returns a minimal cobra command tree representative of grim
// for use in buildShellInit tests. Using the real rootCmd would require full
// viper/config initialisation which is unnecessary here.
func minimalRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "grim", Short: "test"}
	root.AddCommand(&cobra.Command{Use: "shell"})
	root.AddCommand(&cobra.Command{Use: "daemon"})
	root.AddCommand(&cobra.Command{Use: "sync"})
	return root
}

func TestBuildShellInit_bash_includes_grimoire_hooks(t *testing.T) {
	args, env, cleanup, err := buildShellInit("/bin/bash", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()

	require.NotEmpty(t, args, "bash init must return --init-file arg")
	assert.Empty(t, env)

	require.Equal(t, "--init-file", args[0])
	content, readErr := os.ReadFile(args[1])
	require.NoError(t, readErr)

	assert.Contains(t, string(content), "GRIMOIRE_PANE_ID", "should source grimoire hooks")
}

func TestBuildShellInit_bash_includes_completion(t *testing.T) {
	args, _, cleanup, err := buildShellInit("/bin/bash", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()

	content, readErr := os.ReadFile(args[1])
	require.NoError(t, readErr)

	// cobra's bash completion script always defines __start_<cmd> and _<cmd>
	assert.Contains(t, string(content), "__start_grim", "bash init must include grim completion function")
}

func TestBuildShellInit_bash_registers_binary_path(t *testing.T) {
	const grimExe = "/tmp/grim-test-binary"
	args, _, cleanup, err := buildShellInit("/bin/bash", minimalRootCmd(), grimExe)
	require.NoError(t, err)
	defer cleanup()

	content, readErr := os.ReadFile(args[1])
	require.NoError(t, readErr)

	// Must bind completion to the full binary path so `Tab` works when invoked
	// as /tmp/grim or /usr/local/bin/grim, not just the bare `grim` name.
	assert.Contains(t, string(content), `complete -o default -F __start_grim "`+grimExe+`"`,
		"bash init must register completion for the binary's full path")
}

func TestBuildShellInit_zsh_includes_grimoire_hooks(t *testing.T) {
	_, env, cleanup, err := buildShellInit("/bin/zsh", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()

	// Find ZDOTDIR value
	var zdotdir string
	for _, e := range env {
		if strings.HasPrefix(e, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(e, "ZDOTDIR=")
		}
	}
	require.NotEmpty(t, zdotdir, "zsh init must set ZDOTDIR")

	content, readErr := os.ReadFile(zdotdir + "/.zshrc") //nolint:gosec // G304: test-controlled temp path
	require.NoError(t, readErr)

	assert.Contains(t, string(content), "GRIMOIRE_PANE_ID", "should source grimoire hooks")
}

func TestBuildShellInit_zsh_includes_completion(t *testing.T) {
	_, env, cleanup, err := buildShellInit("/bin/zsh", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()

	var zdotdir string
	for _, e := range env {
		if strings.HasPrefix(e, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(e, "ZDOTDIR=")
		}
	}
	require.NotEmpty(t, zdotdir)

	content, readErr := os.ReadFile(zdotdir + "/.zshrc") //nolint:gosec // G304: test-controlled temp path
	require.NoError(t, readErr)

	// cobra's zsh completion script always defines _<cmd> function
	assert.Contains(t, string(content), "_grim", "zsh init must include grim completion function")
}

func TestBuildShellInit_zsh_registers_binary_path(t *testing.T) {
	const grimExe = "/tmp/grim-test-binary"
	_, env, cleanup, err := buildShellInit("/bin/zsh", minimalRootCmd(), grimExe)
	require.NoError(t, err)
	defer cleanup()

	var zdotdir string
	for _, e := range env {
		if strings.HasPrefix(e, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(e, "ZDOTDIR=")
		}
	}
	require.NotEmpty(t, zdotdir)

	content, readErr := os.ReadFile(zdotdir + "/.zshrc") //nolint:gosec // G304: test-controlled temp path
	require.NoError(t, readErr)

	assert.Contains(t, string(content), `compdef _grim "`+grimExe+`"`,
		"zsh init must register completion for the binary's full path")
}

func TestBuildShellInit_zsh_calls_compinit(t *testing.T) {
	_, env, cleanup, err := buildShellInit("/bin/zsh", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()

	var zdotdir string
	for _, e := range env {
		if strings.HasPrefix(e, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(e, "ZDOTDIR=")
		}
	}
	require.NotEmpty(t, zdotdir)

	content, readErr := os.ReadFile(zdotdir + "/.zshrc") //nolint:gosec // G304: test-controlled temp path
	require.NoError(t, readErr)

	assert.Contains(t, string(content), "compinit", "zsh init must call compinit for completion to work")
}

func TestBuildShellInit_other_shell_no_error(t *testing.T) {
	args, env, cleanup, err := buildShellInit("/bin/sh", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)
	defer cleanup()
	assert.Empty(t, args)
	assert.Empty(t, env)
}

func TestBuildShellInit_cleanup_removes_bash_file(t *testing.T) {
	args, _, cleanup, err := buildShellInit("/bin/bash", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)

	path := args[1]
	_, statErr := os.Stat(path)
	require.NoError(t, statErr, "init file must exist before cleanup")

	cleanup()
	_, statErr = os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "init file must be removed after cleanup")
}

func TestBuildShellInit_cleanup_removes_zsh_dir(t *testing.T) {
	_, env, cleanup, err := buildShellInit("/bin/zsh", minimalRootCmd(), "/usr/local/bin/grim")
	require.NoError(t, err)

	var zdotdir string
	for _, e := range env {
		if strings.HasPrefix(e, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(e, "ZDOTDIR=")
		}
	}
	require.NotEmpty(t, zdotdir)

	cleanup()
	_, statErr := os.Stat(zdotdir)
	assert.True(t, os.IsNotExist(statErr), "ZDOTDIR must be removed after cleanup")
}
