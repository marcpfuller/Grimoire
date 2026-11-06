# Go Implementation Guidance

Apply this profile alongside the shared implementation gates for changes that
materially affect Go code or Go build behavior.

## Tool Readiness

Check only tools required by the task:

```bash
go version
golangci-lint --version
mockery --version
grep '^module ' go.mod
```

Do not install or upgrade missing tools silently. In Grimoire, inspect the repository
configuration before choosing between a standalone Mockery binary and the configured
`go tool mockery` version.

## Source Context

Use `gopls` first for workspace symbols, definitions, implementations, references,
call hierarchy, types, and diagnostics. Use `rg` for non-Go artifacts, exact
literals, generated files, or when `gopls` cannot answer. Verify Go symbols found by
text search with `gopls` before relying on them.

For a Grimoire change that affects the Grimoire-Ghostwriter contract, invoke
`$ghostwriter-context`. Treat its result as the related-repository portion of Gate 1
and do not repeat its graph or source work.

Escalate a broad local Go question to `$graphify` only when the user requests it or
combined `gopls` and focused text evidence cannot establish a material cross-package
relationship.

## Implementation and Verification

Use the Go profile from `$testing-standards`. Apply `$typed-state-first` when typed
structs or methods may already contain a value being read from loose state.

Run the repository's applicable checks, commonly:

```bash
go fmt ./...
golangci-lint run ./...
go test -race -cover ./...
govulncheck ./...
```

For concise Grimoire verification, run
`.agents/skills/implementation-workflow/scripts/verify-go.sh` from the repository
root. It stores full logs under `.context/agent-verification/` and prints status plus
a bounded failure tail. Use verbose tests only to diagnose a failure.

For a related frontend change, also use the repository's configured package-manager
test and build commands.
