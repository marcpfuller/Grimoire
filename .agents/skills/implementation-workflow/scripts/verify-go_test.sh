#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
verify_script="$script_dir/verify-go.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/verify-go-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

fake_bin="$test_root/bin"
repo="$test_root/repo"
call_log="$test_root/calls.log"
mkdir -p "$fake_bin" "$repo"
git -C "$repo" init -q

create_fake_command() {
  command_name="$1"
  command_path="$fake_bin/$command_name"

  # These expressions must remain literal until the generated fake command runs.
  # shellcheck disable=SC2016
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -u' \
    'command_name="${0##*/}"' \
    'printf "%s" "$command_name" >> "$FAKE_CALL_LOG"' \
    'printf " %s" "$@" >> "$FAKE_CALL_LOG"' \
    'printf "\n" >> "$FAKE_CALL_LOG"' \
    'if [ "$command_name" = govulncheck ] && [ "${FAIL_GOVULNCHECK:-0}" = 1 ]; then' \
    '  printf "vulnerability found\n" >&2' \
    '  exit 1' \
    'fi' > "$command_path"
  chmod +x "$command_path"
}

create_fake_command go
create_fake_command golangci-lint
create_fake_command govulncheck

success_output="$(
  cd "$repo"
  FAKE_CALL_LOG="$call_log" PATH="$fake_bin:$PATH" "$verify_script"
)"

expected_calls="$(printf '%s\n' \
  'go fmt ./...' \
  'golangci-lint run ./...' \
  'go test -race -cover ./...' \
  'govulncheck ./...')"
actual_calls="$(<"$call_log")"
[ "$actual_calls" = "$expected_calls" ] || fail "unexpected checks:\n$actual_calls"
printf '%s\n' "$success_output" | grep -Fq 'PASS govulncheck' ||
  fail 'successful govulncheck was not reported'

: > "$call_log"
failure_output="$test_root/failure-output.log"
if (
  cd "$repo"
  FAIL_GOVULNCHECK=1 FAKE_CALL_LOG="$call_log" PATH="$fake_bin:$PATH" "$verify_script"
) > "$failure_output" 2>&1; then
  fail 'verify-go.sh succeeded after govulncheck failed'
fi
grep -Fq 'FAIL govulncheck' "$failure_output" || fail 'govulncheck failure was not reported'
grep -Fq 'vulnerability found' "$repo/.context/agent-verification/govulncheck.log" ||
  fail 'govulncheck output was not retained in its log'

printf 'PASS verify-go.sh runs and enforces govulncheck\n'
