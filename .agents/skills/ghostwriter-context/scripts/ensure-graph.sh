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

valid_graph() {
    local output_root=$1
    local graph_root="$output_root/graphify-out"
    [[ -s "$graph_root/graph.json" ]] || return 1
    [[ -s "$graph_root/.graphify_root" ]] || return 1
    [[ "$(resolve_dir "$(<"$graph_root/.graphify_root")")" == "$source_root" ]]
}

emit_result() {
    local status=$1
    local output_root=$2
    printf 'GRAPHIFY_STATUS=%s\n' "$status"
    printf 'GRAPHIFY_SOURCE_ROOT=%s\n' "$source_root"
    printf 'GRAPHIFY_REVISION=%s\n' "$revision"
    printf 'GRAPHIFY_GRAPH=%s\n' "$output_root/graphify-out/graph.json"
}

[[ -d "$source_root" ]] || fail "Ghostwriter repository not found: $source_root"
source_root=$(resolve_dir "$source_root") || fail "cannot resolve repository: $source_root"
git -C "$source_root" rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
    fail "not a Git worktree: $source_root"

if [[ -n "$(git -C "$source_root" status --porcelain)" ]]; then
    fail "source worktree is dirty; use a clean Ghostwriter worktree for revision-pinned context"
fi

revision=$(git -C "$source_root" rev-parse HEAD)

if valid_graph "$source_root"; then
    emit_result "reused-repository" "$source_root"
    exit 0
fi

output_root="$cache_root/ghostwriter/$revision"
if valid_graph "$output_root"; then
    emit_result "reused-cache" "$output_root"
    exit 0
fi

command -v graphify >/dev/null 2>&1 ||
    fail "graphify is not installed; install or enable it before gathering Ghostwriter context"

mkdir -p "$output_root"
lock_dir="$output_root/.ensure-graph.lock"
waited=0
until mkdir "$lock_dir" 2>/dev/null; do
    if valid_graph "$output_root"; then
        emit_result "reused-cache" "$output_root"
        exit 0
    fi
    ((waited += 1))
    if ((waited >= 600)); then
        fail "timed out waiting for another Graphify build: $lock_dir"
    fi
    sleep 1
done
trap 'rmdir "$lock_dir" 2>/dev/null || true' EXIT

if valid_graph "$output_root"; then
    emit_result "reused-cache" "$output_root"
    exit 0
fi

graphify extract "$source_root" --out "$output_root" --code-only --timing
valid_graph "$output_root" || fail "Graphify completed without a valid graph for $source_root"
emit_result "built" "$output_root"
