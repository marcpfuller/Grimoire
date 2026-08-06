#!/usr/bin/env bash
set -euo pipefail

source_root=""
output_root=""
mode="mutable"
label="code-graph"
directed="false"

usage() {
    cat <<'EOF'
Usage: ensure-code-graph.sh --source-root PATH [options]

Options:
  --output-root PATH  Parent directory for graphify-out (default: source root)
  --immutable         Reuse a valid graph without running an incremental update
  --directed          Mark extracted source-to-target links as a directed graph
  --label NAME        Log filename label
EOF
}

fail() {
    printf 'ensure-code-graph: %s\n' "$*" >&2
    exit 1
}

resolve_dir() {
    (cd "$1" 2>/dev/null && pwd -P)
}

while (($# > 0)); do
    case "$1" in
        --source-root)
            (($# >= 2)) || fail "--source-root requires a path"
            source_root=$2
            shift 2
            ;;
        --output-root)
            (($# >= 2)) || fail "--output-root requires a path"
            output_root=$2
            shift 2
            ;;
        --immutable)
            mode="immutable"
            shift
            ;;
        --directed)
            directed="true"
            shift
            ;;
        --label)
            (($# >= 2)) || fail "--label requires a value"
            label=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail "unknown argument: $1"
            ;;
    esac
done

[[ -n "$source_root" ]] || fail "--source-root is required"
[[ -d "$source_root" ]] || fail "source root not found: $source_root"
source_root=$(resolve_dir "$source_root") || fail "cannot resolve source root"

output_root=${output_root:-$source_root}
mkdir -p "$output_root"
output_root=$(resolve_dir "$output_root") || fail "cannot resolve output root"

command -v graphify >/dev/null 2>&1 ||
    fail "graphify is not installed; install or enable it before building a graph"
command -v python3 >/dev/null 2>&1 || fail "python3 is required for graph summary output"

safe_label=$(printf '%s' "$label" | tr -cs 'A-Za-z0-9._-' '-')
log_dir=${GRAPHIFY_LOG_DIR:-$PWD/.context/graphify}
mkdir -p "$log_dir"
run_id="$(date -u '+%Y%m%dT%H%M%SZ')-$$"
log_path="$log_dir/${safe_label}-${run_id}.log"
diagnostics_path="$log_dir/${safe_label}-${run_id}-diagnostics.json"
graph_root="$output_root/graphify-out"
graph_path="$graph_root/graph.json"
diagnostics_cache="$graph_root/.ensure-code-graph-diagnostics.json"

valid_graph() {
    [[ -s "$graph_path" ]] || return 1
    [[ -s "$graph_root/.graphify_root" ]] || return 1
    local recorded_root
    recorded_root=$(resolve_dir "$(<"$graph_root/.graphify_root")") || return 1
    [[ "$recorded_root" == "$source_root" ]]
}

lock_dir="$output_root/.ensure-code-graph.lock"
waited=0
until mkdir "$lock_dir" 2>/dev/null; do
    ((waited += 1))
    if ((waited >= 600)); then
        fail "timed out waiting for graph lock: $lock_dir"
    fi
    sleep 1
done
trap 'rmdir "$lock_dir" 2>/dev/null || true' EXIT

status=""
if valid_graph && [[ "$mode" == "immutable" ]]; then
    status="reused"
    : >"$log_path"
elif valid_graph; then
    status="updated"
    if [[ "$output_root" == "$source_root" ]]; then
        graphify update "$source_root" >"$log_path" 2>&1 || {
            tail -40 "$log_path" >&2
            fail "Graphify update failed; full log: $log_path"
        }
    else
        GRAPHIFY_OUT="$graph_root" graphify update "$source_root" >"$log_path" 2>&1 || {
            tail -40 "$log_path" >&2
            fail "Graphify update failed; full log: $log_path"
        }
    fi
else
    status="built"
    graphify extract "$source_root" --out "$output_root" --code-only --timing --force >"$log_path" 2>&1 || {
        tail -40 "$log_path" >&2
        fail "Graphify build failed; full log: $log_path"
    }
fi

valid_graph || fail "Graphify completed without a valid graph for $source_root"
if [[ "$directed" == "true" ]]; then
    python3 - "$graph_path" <<'PY'
import json
import os
import sys
from pathlib import Path

path = Path(sys.argv[1])
data = json.loads(path.read_text(encoding="utf-8"))
if not data.get("directed", False):
    data["directed"] = True
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    os.replace(temporary, path)
PY
fi
if [[ -s "$diagnostics_cache" && "$diagnostics_cache" -nt "$graph_path" ]]; then
    cp "$diagnostics_cache" "$diagnostics_path"
else
    graphify diagnose multigraph --graph "$graph_path" --json >"$diagnostics_path" 2>>"$log_path" ||
        fail "Graphify diagnostics failed; full log: $log_path"
    cp "$diagnostics_path" "$diagnostics_cache"
fi

python3 - "$status" "$source_root" "$graph_path" "$log_path" "$diagnostics_path" <<'PY'
import json
import sys
from pathlib import Path

status, source_root, graph_path, log_path, diagnostics_path = sys.argv[1:]
graph = json.loads(Path(graph_path).read_text(encoding="utf-8"))
diagnostics = json.loads(Path(diagnostics_path).read_text(encoding="utf-8"))
summary = diagnostics.get("summary", {})
warning_fields = (
    "non_object_edges",
    "missing_endpoint_edges",
    "dangling_endpoint_edges",
    "exact_duplicate_edges",
    "directed_same_endpoint_collapsed_edges",
)
warnings = sum(int(summary.get(field, 0) or 0) for field in warning_fields)

print(f"GRAPHIFY_STATUS={status}")
print(f"GRAPHIFY_SOURCE_ROOT={source_root}")
print(f"GRAPHIFY_GRAPH={graph_path}")
print(f"GRAPHIFY_NODES={len(graph.get('nodes', []))}")
print(f"GRAPHIFY_EDGES={len(graph.get('links', []))}")
print(f"GRAPHIFY_DIRECTED={str(bool(graph.get('directed', False))).lower()}")
print(f"GRAPHIFY_WARNINGS={warnings}")
print("GRAPHIFY_RUN_SEMANTIC_TOKENS=0")
print(f"GRAPHIFY_LOG={log_path}")
PY
