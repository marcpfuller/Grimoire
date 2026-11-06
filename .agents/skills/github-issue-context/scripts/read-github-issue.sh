#!/usr/bin/env bash

set -eu

usage() {
	printf 'usage: %s ISSUE_NUMBER\n' "${0##*/}" >&2
}

if [ "$#" -ne 1 ]; then
	usage
	exit 2
fi

issue_number="$1"
if [[ ! "$issue_number" =~ ^[1-9][0-9]*$ ]]; then
	printf 'error: ISSUE_NUMBER must be a positive decimal integer\n' >&2
	usage
	exit 2
fi

issue_fields='number,title,state,author,assignees,labels,milestone,body,comments,url'

read_issue() {
	env GH_PROMPT_DISABLED=1 GH_PAGER=cat PAGER=cat \
		gh issue view "$issue_number" --json "$issue_fields"
}

if output="$(read_issue 2>&1)"; then
	printf '%s\n' "$output"
	exit 0
else
	status="$?"
fi

# Local Conductor sessions may inject a GH_TOKEN that has narrower access than
# the user's authenticated gh keychain account. Retry without token overrides;
# cloud sessions depend on their injected token and must not discard it.
if [ "${CONDUCTOR_IS_LOCAL-}" = 1 ] && { [ -n "${GH_TOKEN-}" ] || [ -n "${GITHUB_TOKEN-}" ]; }; then
	if output="$(env -u GH_TOKEN -u GITHUB_TOKEN GH_PROMPT_DISABLED=1 GH_PAGER=cat PAGER=cat \
		gh issue view "$issue_number" --json "$issue_fields" 2>&1)"; then
		printf '%s\n' "$output"
		exit 0
	else
		status="$?"
	fi
fi

printf '%s\n' "$output" >&2
exit "$status"
