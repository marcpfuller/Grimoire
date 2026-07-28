package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/marcpfuller/grimoire/internal/cast"
	"github.com/marcpfuller/grimoire/internal/db"
)

var castCmd = &cobra.Command{
	Use:   "cast <id> [id...]",
	Short: "Synthesize an Asciinema v2 .cast file from selected commands",
	Long: `Synthesize an Asciinema v2 .cast file from one or more recorded commands.

All IDs must belong to the same pane session and must be supplied in
chronological (ascending seq) order. Non-contiguous commands are supported —
commands between the selected IDs are simply omitted from the recording.

Example:
  grim cast abc0001
  grim cast abc0001 abc0025
  grim cast abc0001 abc0005 abc0012 --out ~/recordings/recon.cast`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCast,
}

var castOutFile string

func init() {
	castCmd.Flags().StringVar(&castOutFile, "out", "",
		"output file path (default: <cast.dir>/grimoire-<timestamp>.cast)")
}

func runCast(cmd *cobra.Command, args []string) error {
	if err := initConfig(); err != nil {
		return err
	}

	store, err := db.Open(viper.GetString("db.path"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close() //nolint:errcheck

	outDir := viper.GetString("cast.dir")
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(viper.GetString("db.path")), "casts")
	}

	cfg := cast.Params{
		ShortIDs: args,
		OutDir:   outDir,
		OutFile:  castOutFile,
	}

	path, err := cast.Synthesize(cmd.Context(), store, cfg)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
	return nil
}
