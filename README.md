# Grimoire
Grimoire is a local Red Team daemon that captures terminal activity for reporting. By wrapping POSIX PTYs and native shell hooks, it turns chaotic command output into immutable, queryable records that can feed polished evidence, screenshots, and client reports.

## Requirements

- Go 1.25+
- Node.js 20+ (for the frontend build)

## Build

The frontend must be compiled before the Go binary, because `web/dist/` is embedded at build time:

```bash
cd web && npm ci && npm run build && cd ..
go build -o grim ./cmd/grim
```

Or use the Makefile targets:

```bash
make typegen   # regenerate web/src/api/generated.ts from Go structs
make build     # go build -o dist/grim
```

> **Note:** `web/dist/` and `web/src/api/generated.ts` are gitignored build artifacts. Run `make typegen` after changing any Go response struct, and run the frontend build step before `go build`.

Move the binary somewhere on your `$PATH`:

```bash
mv grim /usr/local/bin/grim
```

## Configuration

Grimoire reads `config/config.yaml` by default. Copy and edit the example:

```bash
cp config/config.yaml ~/.grim/config.yaml
```

Key settings:

| Key | Default | Description |
|-----|---------|-------------|
| `db.path` | `~/.grim/grimoire.db` | Path to the SQLite database |
| `server.host` | `127.0.0.1` | Daemon listen address |
| `server.port` | `7777` | Daemon listen port |
| `ghostwriter.url` | `""` | Ghostwriter instance URL |
| `ghostwriter.token` | `""` | Ghostwriter API token |
| `cast.dir` | `""` | Directory for `.cast` output (defaults to `casts/` next to `db.path`) |
| `sync.interval_seconds` | `60` | How often the daemon pushes pending commands to Ghostwriter |
| `log.dir` | `.` | Directory for the log file |

Pass a custom config with `--config`:

```bash
grim --config /path/to/config.yaml <command>
```

## Install as a System Service

Install and start the Grimoire daemon as a background service (launchd on macOS, systemd on Linux):

```bash
grim service install
```

Check its status:

```bash
grim service status
```

Restart after a config change:

```bash
grim service restart
```

Remove the service:

```bash
grim service uninstall
```

## Run the Daemon Manually

```bash
grim daemon start
```

## Web UI

When the daemon is running, open `http://127.0.0.1:7777` in a browser. The React dashboard is compiled into the binary — no separate server or Docker container is needed.

The UI provides:

- **Session Explorer** — commands grouped by pane, with timestamps and duration
- **Command Detail** — raw terminal output, operator annotations (title, description, MITRE tags), and one-click Asciinema export
- **Tags View** — all tagged commands across all sessions
- **Settings** — edit `ghostwriter.url`, `ghostwriter.token`, and sync interval in-browser; changes are written back to `config.yaml`
- **GW Status** — the status bar shows live Ghostwriter connectivity; click it to jump to Settings

The UI runs in mock mode during development (`VITE_USE_MOCK=true`) so the daemon is not required for frontend iteration.

## Start an Instrumented Shell

```bash
grim shell --project <ghostwriter-project-id>
```

This spawns your default shell wrapped in Grimoire's PTY capture. Every command you run is recorded, sequenced, and synced to Ghostwriter.

## Tag a Command

Attach labels to a recorded command by its short ID (shown in the shell prompt):

```bash
grim tag --id a1b2c3d "Dumped hashes"
grim tag --id a1b2c3d "Credential Access" "TA0006"
```

## Synthesize a `.cast` Recording

Export one or more cherry-picked commands as an [Asciinema v2](https://docs.asciinema.org/manual/asciicast/v2/) file:

```bash
grim cast <id> [id...]
```

Rules:
- All IDs must belong to the same pane session.
- IDs may be supplied in any order and duplicates are ignored — the daemon sorts by sequence number and deduplicates before synthesis.
- Commands between selected IDs are skipped — non-contiguous picks are fine.

The output path defaults to `<cast.dir>/grimoire-<YYYYMMDD-HHMMSS>.cast`.

```bash
# Single command
grim cast abc0001

# Sequential range
grim cast abc0001 abc0025

# Cherry-pick specific commands (skipping ones in between)
grim cast abc0001 abc0005 abc0012

# IDs may be passed in any order
grim cast abc0012 abc0001 abc0005

# Explicit output path
grim cast abc0001 abc0012 --out ~/recordings/recon.cast
```

Multi-command export is also available from the Web UI — select commands using the checkboxes that appear on hover in the Session Explorer, then click **Export .cast** at the bottom of the sidebar.

## View Shell History

List recorded commands for the current pane (or all panes):

```bash
# Current pane (reads $GRIMOIRE_PANE_ID)
grim history

# Specific pane
grim history --pane <pane-id>

# All panes
grim history --all
```

Output is a tab-aligned table: `ID | TIMESTAMP | DURATION | EXIT | COMMAND`.

## Manage Config at Runtime

View or update daemon configuration without restarting:

```bash
# Print all config keys
grim config get

# Print a single key
grim config get sync.interval_seconds

# Update a key — takes effect immediately
grim config set sync.interval_seconds 60
grim config set ghostwriter.url https://ghostwriter.example.com
grim config set ghostwriter.token gw_...
grim config set log.level debug
```

Changes are written back to `config.yaml` and applied live:

| Key | Live effect |
|-----|-------------|
| `sync.interval_seconds` | Sync loop ticker resets immediately |
| `ghostwriter.url` / `ghostwriter.token` | Ghostwriter pusher is swapped in-place (no restart needed) |
| `log.level` | `slog.LevelVar` updated immediately; change is logged |
| `cast.dir` | Used on the next export |

Settings are also editable from the **Settings** view in the Web UI.
