#!/usr/bin/env bash

set -eu

usage() {
	printf 'usage: %s PR_NUMBER\n' "${0##*/}" >&2
}

if [ "$#" -ne 1 ]; then
	usage
	exit 2
fi

pr_number="$1"
if [[ ! "$pr_number" =~ ^[1-9][0-9]*$ ]]; then
	printf 'error: PR_NUMBER must be a positive decimal integer\n' >&2
	usage
	exit 2
fi

# shellcheck disable=SC2016 # GraphQL variables must remain literal for gh api.
query='
query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      comments(first: 100) {
        pageInfo { hasNextPage endCursor }
        nodes {
          author { login }
          body
          url
        }
      }
      reviews(first: 100) {
        pageInfo { hasNextPage endCursor }
        nodes {
          author { login }
          state
          body
          url
        }
      }
      reviewThreads(first: 100) {
        pageInfo { hasNextPage endCursor }
        nodes {
          isOutdated
          isResolved
          path
          line
          startLine
          diffSide
          startDiffSide
          comments(first: 100) {
            pageInfo { hasNextPage endCursor }
            nodes {
              author { login }
              body
              url
              diffHunk
            }
          }
        }
      }
    }
  }
}
'

# shellcheck disable=SC2016 # jq expressions must remain literal for gh api.
output_filter='
  def clean_text:
    gsub("\r\n"; "\n")
    | gsub("\r"; "\n")
    | gsub("&nbsp;"; " ")
    | gsub("&quot;"; "\"")
    | gsub("&#34;"; "\"")
    | gsub("&#x22;"; "\"")
    | gsub("&apos;"; "\u0027")
    | gsub("&#39;"; "\u0027")
    | gsub("&#x27;"; "\u0027")
    | gsub("&lt;"; "<")
    | gsub("&#60;"; "<")
    | gsub("&#x3[Cc];"; "<")
    | gsub("&gt;"; ">")
    | gsub("&#62;"; ">")
    | gsub("&#x3[Ee];"; ">")
    | gsub("&amp;"; "&");
  def author: (.author.login // "unknown");
  def location($thread):
    $thread.path + ":" + (($thread.line // $thread.startLine // 0) | tostring);
  def diff_hunk($value):
    if ($value // "") == "" then ""
    else "\n\n```diff\n" + ($value | clean_text) + "\n```"
    end;
  def section($heading; $items):
    if ($items | length) == 0 then ""
    else "## " + $heading + "\n\n" + ($items | join("\n\n"))
    end;
  .data.repository.pullRequest as $pr
  | [
      section("Conversation comments"; [
          $pr.comments.nodes[]
          | select((.body // "") != "")
          | "### @" + author + "\n" + (.body | clean_text)
        ]),
      section("Submitted review comments"; [
          $pr.reviews.nodes[]
          | select((.body // "") != "")
          | "### @" + author + " (" + .state + ")\n" + (.body | clean_text)
        ]),
      section("Inline review comments"; [
          $pr.reviewThreads.nodes[]
          | . as $thread
          | $thread.comments.nodes[]
          | select((.body // "") != "")
          | select((((.author.login // "") | ascii_downcase) != "copilot-pull-request-reviewer[bot]"))
          | "### @" + author + " — " + location($thread)
            + "\n" + (.body | clean_text) + diff_hunk(.diffHunk)
        ]),
      section("Current Copilot suggested code changes"; [
        $pr.reviewThreads.nodes[]
        | select(.isOutdated == false)
        | . as $thread
        | $thread.comments.nodes[]
        | select((((.author.login // "") | ascii_downcase) == "copilot-pull-request-reviewer[bot]"))
        | "### " + location($thread) + "\n"
          + (.body | clean_text) + diff_hunk(.diffHunk)
      ]),
      (if $pr.comments.pageInfo.hasNextPage
          or $pr.reviews.pageInfo.hasNextPage
          or $pr.reviewThreads.pageInfo.hasNextPage
       then "## Incomplete context\n\n- At least one comment collection has more than 100 entries."
       else ""
       end)
    ]
  | map(select(. != ""))
  | join("\n\n")
'

run_gh() {
	drop_token="$1"
	shift

	if [ "$drop_token" = 1 ]; then
		env -u GH_TOKEN -u GITHUB_TOKEN GH_PROMPT_DISABLED=1 GH_PAGER=cat PAGER=cat gh "$@"
	else
		env GH_PROMPT_DISABLED=1 GH_PAGER=cat PAGER=cat gh "$@"
	fi
}

read_pr() {
	drop_token="$1"

	repo_name="$(run_gh "$drop_token" repo view --json nameWithOwner --jq .nameWithOwner)"
	case "$repo_name" in
		*/*)
			owner="${repo_name%%/*}"
			name="${repo_name#*/}"
			;;
		*)
			printf 'error: gh repo view returned an invalid repository name\n' >&2
			return 1
			;;
	esac

	run_gh "$drop_token" api graphql \
		-f query="$query" \
		-F owner="$owner" \
		-F name="$name" \
		-F number="$pr_number" \
		--jq "$output_filter"
}

if output="$(read_pr 0 2>&1)"; then
	printf '%s\n' "$output"
	exit 0
else
	status="$?"
fi

# Local Conductor sessions may inject a GH_TOKEN that has narrower access than
# the user's authenticated gh keychain account. Retry without token overrides;
# cloud sessions depend on their injected token and must not discard it.
if [ "${CONDUCTOR_IS_LOCAL-}" = 1 ] && { [ -n "${GH_TOKEN-}" ] || [ -n "${GITHUB_TOKEN-}" ]; }; then
	if output="$(read_pr 1 2>&1)"; then
		printf '%s\n' "$output"
		exit 0
	else
		status="$?"
	fi
fi

printf '%s\n' "$output" >&2
exit "$status"
