#!/usr/bin/env bash

set -u

script_dir="$(cd -P -- "$(dirname -- "$0")" && pwd)"
script="${script_dir}/read-github-pr.sh"
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

if [ "$1" = repo ]; then
	printf '%s\n' 'acme/widgets'
	exit 0
fi

if [ "$1" = api ] && [ "$2" = graphql ]; then
	shift 2
	while [ "$#" -gt 0 ]; do
		if [ "$1" = --jq ]; then
			output_filter="$2"
			break
		fi
		shift
	done

	if [ -z "${output_filter-}" ]; then
		printf 'missing jq output filter\n' >&2
		exit 1
	fi

	fixture='{"data":{"repository":{"pullRequest":{"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"author":{"login":"octocat"},"body":"Use &lt;example&gt; &amp; test.\\r\\nNext line &#39;quoted&#39;.","url":"https://example.test/comment"}]},"reviews":{"pageInfo":{"hasNextPage":false},"nodes":[]},"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}'
	fixture="${fixture//\\\\r\\\\n/$'\\r\\n'}"
	printf '%s\n' "$fixture" | jq -r "$output_filter"
	exit 0
fi

printf 'unexpected gh invocation\n' >&2
exit 1
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
assert_rejected_without_gh nonnumeric '42; gh pr merge 42'
assert_rejected_without_gh zero 0
assert_rejected_without_gh extra 42 43

call_log="${test_dir}/valid.calls"
output="$(env -u GH_TOKEN -u GITHUB_TOKEN GH_CALL_LOG="$call_log" PATH="${fake_bin}:$PATH" "$script" 42)" || fail 'valid: command failed'

[ "$output" = '## Conversation comments

### @octocat
Use <example> & test.
Next line '\''quoted'\''.' ] || fail 'valid: comment text was not normalized'
rg -F -q 'repo' "$call_log" || fail 'valid: gh repo view was not called'
rg -F -q 'api' "$call_log" || fail 'valid: gh api was not called'
rg -F -q 'graphql' "$call_log" || fail 'valid: GraphQL endpoint was not used'
rg -F -q 'number=42' "$call_log" || fail 'valid: PR number was not passed as a GraphQL variable'
rg -F -q -- '--jq' "$call_log" || fail 'valid: output was not filtered'
rg -F -q 'Current Copilot suggested code changes' "$call_log" || fail 'valid: current Copilot changes were not selected'
rg -F -q 'clean_text' "$call_log" || fail 'valid: comment text was not normalized'
rg -F -q 'gsub("&amp;"; "&")' "$call_log" || fail 'valid: HTML entities were not decoded'
if rg -F -q 'pullRequest: $pr' "$call_log"; then fail 'valid: untrimmed pull request data was selected'; fi

fallback_log="${test_dir}/fallback.calls"
fallback_stderr="${test_dir}/fallback.stderr"
fallback_output="$(CONDUCTOR_IS_LOCAL=1 GH_TOKEN=unusable FAKE_GH_FAIL_WITH_TOKEN=1 GH_CALL_LOG="$fallback_log" PATH="${fake_bin}:$PATH" "$script" 42 2>"$fallback_stderr")" || fail 'fallback: command failed'

[ "$fallback_output" = '## Conversation comments

### @octocat
Use <example> & test.
Next line '\''quoted'\''.' ] || fail 'fallback: comment text was not normalized'
[ ! -s "$fallback_stderr" ] || fail 'fallback: recoverable authentication error was emitted'
[ "$(rg -F -c 'token=set' "$fallback_log")" -eq 1 ] || fail 'fallback: expected one token-backed attempt'
[ "$(rg -F -c 'token=unset' "$fallback_log")" -eq 2 ] || fail 'fallback: expected token-free repo and API calls'

printf 'PASS read-github-pr\n'
