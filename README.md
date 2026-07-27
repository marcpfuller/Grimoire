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
| `db.path` | `grimoire.db` | Path to the SQLite database |
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

Export a range of commands as an [Asciinema v2](https://docs.asciinema.org/manual/asciicast/v2/) file from the CLI:

```bash
grim cast <first-id> <last-id>
```

Both IDs must belong to the same pane session, and `first-id` must precede `last-id` in sequence order. The output path defaults to `<cast.dir>/grimoire-<timestamp>.cast`.

Override the output path for a one-off export:

```bash
grim cast abc0001 abc0025 --out ~/recordings/recon.cast
```

Single-command exports are also supported from the Web UI **Command Detail** panel — click **Export .cast** to write the file to `cast.dir` and see the path in a toast notification.
