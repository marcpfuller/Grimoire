# Grimoire Agent Guide

These instructions apply to the whole repository. Grimoire is a security-sensitive
Go daemon with a React frontend. It captures terminal activity, stores local data,
serves an authenticated HTTP API, and can synchronize records to Ghostwriter.

## Implementation Workflow

For any change to code, tests, configuration, build behavior, or generated behavior,
invoke `$go-implementation-workflow` and complete its four gates in order:

1. Source context
2. Settled plan
3. TDD implementation
4. Verification and report

Do not edit implementation files before the plan gate is settled. A clear user
request or explicit approval settles decisions it already answers; do not ask the
user to repeat them. Pure questions, reviews, and prose-only documentation changes
do not require the implementation workflow unless they lead to edits.

Do not eagerly load every repo skill. The workflow decides which specialized skills
are needed and records concise reasons for non-applicable gates or subskills.

## Repository Baseline

- Inspect relevant implementations, types, interfaces, callers, tests, and error
  paths before changing behavior.
- Preserve existing architecture and user changes. Keep edits scoped.
- Validate untrusted input with explicit limits. Keep authentication and
  authorization deny-by-default.
- Never log tokens, passwords, terminal secrets, credential-bearing request bodies,
  or secret configuration values.
- Use parameterized SQL, `crypto/rand`, authenticated encryption, constant-time
  secret comparison, narrowly scoped filesystem access, and separate process
  arguments instead of shell interpolation.
- Check every error that affects correctness or safety. Return stable non-sensitive
  external errors while retaining useful wrapped errors internally.

## Toolchain and Generated Prerequisites

```bash
go version
golangci-lint --version
mockery --version
grep '^module ' go.mod
```

This repository uses Mockery v2 through `go tool mockery`. Do not change its major
version without deliberately migrating the configuration and generated output.

Fresh workspaces need ignored generated artifacts before the full Go suite can run:

```bash
npm --prefix web ci
make typegen
make build-frontend
go tool mockery
```

For frontend changes, run `npm --prefix web test -- --run` and
`npm --prefix web run build` in addition to the applicable Go checks.
