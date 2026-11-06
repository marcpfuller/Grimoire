#!/usr/bin/env bash
set -euo pipefail

graph=""
source_root=""
refresh="true"
budget=800
mode="bfs"
contexts=()

usage() {
    cat <<'EOF'
Usage: query-compact.sh --graph PATH [options] "focused graph query"

Options:
  --source-root PATH  Code root to refresh (default: Git root containing graph)
  --no-refresh        Query an immutable/custom graph without updating it
  --budget N       Maximum Graphify result tokens (default: 800; range: 100-4000)
  --context VALUE  Edge-context filter; repeat as needed
  --dfs            Use depth-first traversal
EOF
}

fail() {
    printf 'query-compact: %s\n' "$*" >&2
    exit 1
}

while (($# > 0)); do
    case "$1" in
        --graph)
            (($# >= 2)) || fail "--graph requires a path"
            graph=$2
            shift 2
            ;;
        --source-root)
            (($# >= 2)) || fail "--source-root requires a path"
            source_root=$2
            shift 2
            ;;
        --no-refresh)
            refresh="false"
            shift
            ;;
        --budget)
            (($# >= 2)) || fail "--budget requires a value"
            budget=$2
            shift 2
            ;;
        --context)
            (($# >= 2)) || fail "--context requires a value"
            contexts+=("$2")
            shift 2
            ;;
        --dfs)
            mode="dfs"
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --*)
            fail "unknown argument: $1"
            ;;
        *)
            break
            ;;
    esac
done

[[ -n "$graph" ]] || fail "--graph is required"
[[ "$budget" =~ ^[0-9]+$ ]] || fail "--budget must be an integer"
((budget >= 100 && budget <= 4000)) || fail "--budget must be between 100 and 4000"
(($# > 0)) || fail "a focused query is required"
question=$*

command -v graphify >/dev/null 2>&1 || fail "graphify is not installed"

if [[ "$refresh" == "true" ]]; then
    graph_dir=$(dirname "$graph")
    [[ "$(basename "$graph")" == "graph.json" && "$(basename "$graph_dir")" == "graphify-out" ]] ||
        fail "automatic refresh requires a graphify-out/graph.json path; use --no-refresh for a custom graph"

    output_root=$(cd "$(dirname "$graph_dir")" 2>/dev/null && pwd -P) ||
        fail "cannot resolve graph output root: $(dirname "$graph_dir")"
    graph="$output_root/graphify-out/graph.json"

    if [[ -z "$source_root" ]]; then
        source_root=$(git -C "$output_root" rev-parse --show-toplevel 2>/dev/null) ||
            fail "cannot infer source root; pass --source-root PATH"
    fi
    [[ -d "$source_root" ]] || fail "source root not found: $source_root"
    source_root=$(cd "$source_root" 2>/dev/null && pwd -P) ||
        fail "cannot resolve source root: $source_root"

    script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
    "$script_dir/ensure-code-graph.sh" \
        --source-root "$source_root" \
        --output-root "$output_root" \
        --label pre-query
fi

[[ -s "$graph" ]] || fail "graph not found: $graph"

log_dir=${GRAPHIFY_LOG_DIR:-$PWD/.context/graphify}
mkdir -p "$log_dir"
log_path="$log_dir/query-$(date -u '+%Y%m%dT%H%M%SZ')-$$.log"

args=(query "$question" --graph "$graph" --budget "$budget")
[[ "$mode" == "dfs" ]] && args+=(--dfs)
if ((${#contexts[@]} > 0)); then
    for context in "${contexts[@]}"; do
        args+=(--context "$context")
    done
fi

graphify "${args[@]}" >"$log_path" 2>&1 || {
    tail -40 "$log_path" >&2
    fail "Graphify query failed; full log: $log_path"
}

printf 'GRAPHIFY_QUERY_LOG=%s\n' "$log_path"
cat "$log_path"
