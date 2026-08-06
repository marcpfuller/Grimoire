#!/usr/bin/env bash
set -euo pipefail

source_root=${1:-${GHOSTWRITER_REPO:-$HOME/dev/myrepos/Ghostwriter}}
cache_root=${GRIMOIRE_GRAPHIFY_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/grimoire/graphify}

fail() {
    printf 'ghostwriter-context: %s\n' "$*" >&2
    exit 1
}

resolve_dir() {
    (cd "$1" 2>/dev/null && pwd -P)
}

[[ -d "$source_root" ]] || fail "Ghostwriter repository not found: $source_root"
source_root=$(resolve_dir "$source_root") || fail "cannot resolve repository: $source_root"
git -C "$source_root" rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
    fail "not a Git worktree: $source_root"

if [[ -n "$(git -C "$source_root" status --porcelain)" ]]; then
    fail "source worktree is dirty; use a clean Ghostwriter worktree for revision-pinned context"
fi

revision=$(git -C "$source_root" rev-parse HEAD)
repo_graph="$source_root/graphify-out/graph.json"
repo_marker="$source_root/graphify-out/.graphify_root"
output_root="$cache_root/ghostwriter/$revision"

if [[ -s "$repo_graph" && -s "$repo_marker" ]] &&
    [[ "$(resolve_dir "$(<"$repo_marker")")" == "$source_root" ]]; then
    output_root=$source_root
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ensure_script="$script_dir/../../graphify/scripts/ensure-code-graph.sh"
[[ -x "$ensure_script" ]] || fail "missing Graphify ensure script: $ensure_script"

result=$(
    "$ensure_script" \
        --source-root "$source_root" \
        --output-root "$output_root" \
        --immutable \
        --label "ghostwriter-$revision"
)

printf 'GHOSTWRITER_REVISION=%s\n' "$revision"
printf '%s\n' "$result"
