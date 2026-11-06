#!/usr/bin/env bash

set -u

script_dir="$(cd -P -- "$(dirname -- "$0")" && pwd)"
script="${script_dir}/read-github-issue.sh"
test_dir="$(mktemp -d)" || exit 1
trap 'rm -rf -- "$test_dir"' EXIT HUP INT TERM

fake_bin="${test_dir}/bin"
mkdir -p "$fake_bin"

cat >"${fake_bin}/gh" <<'EOF'
#!/usr/bin/env bash
if [ -n "${GH_TOKEN-}" ]; then
	printf 'token=set\n' >>"$GH_CALL_LOG"
else
	printf 'token=unset\n' >>"$GH_CALL_LOG"
fi
printf '%s\n' "$@" >>"$GH_CALL_LOG"
if [ "${FAKE_GH_FAIL_WITH_TOKEN-}" = 1 ] && [ -n "${GH_TOKEN-}" ]; then
	printf 'token cannot access repository\n' >&2
	exit 1
fi
printf '%s\n' '{"number":42,"title":"Example"}'
EOF
chmod +x "${fake_bin}/gh"

fail() {
	printf 'FAIL %s\n' "$1" >&2
	exit 1
}

assert_rejected_without_gh() {
	name="$1"
	shift
	call_log="${test_dir}/${name}.calls"
	stdout="${test_dir}/${name}.stdout"
	stderr="${test_dir}/${name}.stderr"

	if GH_CALL_LOG="$call_log" PATH="${fake_bin}:$PATH" "$script" "$@" >"$stdout" 2>"$stderr"; then
		fail "${name}: invalid arguments succeeded"
	fi

	[ ! -e "$call_log" ] || fail "${name}: gh was called"
}

assert_rejected_without_gh missing
assert_rejected_without_gh nonnumeric '42; gh issue close 42'
assert_rejected_without_gh zero 0
assert_rejected_without_gh extra 42 43

call_log="${test_dir}/valid.calls"
output="$(env -u GH_TOKEN -u GITHUB_TOKEN GH_CALL_LOG="$call_log" PATH="${fake_bin}:$PATH" "$script" 42)" || fail 'valid: command failed'

[ "$output" = '{"number":42,"title":"Example"}' ] || fail 'valid: stdout was not passed through'

expected_calls="${test_dir}/expected.calls"
cat >"$expected_calls" <<'EOF'
token=unset
issue
view
42
--json
number,title,state,author,assignees,labels,milestone,body,comments,url
EOF

cmp -s "$expected_calls" "$call_log" || fail 'valid: unexpected gh arguments'

fallback_log="${test_dir}/fallback.calls"
fallback_stderr="${test_dir}/fallback.stderr"
fallback_output="$(CONDUCTOR_IS_LOCAL=1 GH_TOKEN=unusable FAKE_GH_FAIL_WITH_TOKEN=1 GH_CALL_LOG="$fallback_log" PATH="${fake_bin}:$PATH" "$script" 42 2>"$fallback_stderr")" || fail 'fallback: command failed'

[ "$fallback_output" = '{"number":42,"title":"Example"}' ] || fail 'fallback: stdout was not passed through'
[ ! -s "$fallback_stderr" ] || fail 'fallback: recoverable authentication error was emitted'

fallback_expected="${test_dir}/fallback-expected.calls"
cat >"$fallback_expected" <<'EOF'
token=set
issue
view
42
--json
number,title,state,author,assignees,labels,milestone,body,comments,url
token=unset
issue
view
42
--json
number,title,state,author,assignees,labels,milestone,body,comments,url
EOF

cmp -s "$fallback_expected" "$fallback_log" || fail 'fallback: expected one keychain retry'

printf 'PASS read-github-issue\n'
