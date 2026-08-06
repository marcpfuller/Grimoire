#!/usr/bin/env bash
set -euo pipefail

graph=""
budget=800
mode="bfs"
contexts=()

usage() {
    cat <<'EOF'
Usage: query-compact.sh --graph PATH [options] "focused graph query"

Options:
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
[[ -s "$graph" ]] || fail "graph not found: $graph"
[[ "$budget" =~ ^[0-9]+$ ]] || fail "--budget must be an integer"
((budget >= 100 && budget <= 4000)) || fail "--budget must be between 100 and 4000"
(($# > 0)) || fail "a focused query is required"
question=$*

command -v graphify >/dev/null 2>&1 || fail "graphify is not installed"

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
