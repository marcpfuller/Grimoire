#!/usr/bin/env bash

set -u

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  printf 'FAIL repository: not inside a Git worktree\n' >&2
  exit 1
}

log_dir="${repo_root}/.context/agent-verification"
mkdir -p "$log_dir"

failed=0

run_check() {
  label="$1"
  shift
  log_file="${log_dir}/${label}.log"

  if (cd "$repo_root" && "$@") >"$log_file" 2>&1; then
    printf 'PASS %s\n' "$label"
    return
  fi

  failed=1
  printf 'FAIL %s (log: %s)\n' "$label" "$log_file" >&2
  tail -80 "$log_file" >&2
}

run_check go-fmt go fmt ./...
run_check golangci-lint golangci-lint run ./...
run_check go-test-race-cover go test -race -cover ./...
run_check govulncheck govulncheck ./...

exit "$failed"
