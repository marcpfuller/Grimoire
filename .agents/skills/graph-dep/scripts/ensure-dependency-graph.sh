#!/usr/bin/env bash
set -euo pipefail

source_root=""
dependency=""
version=""
repository_ref=""

fail() {
    printf 'ensure-dependency-graph: %s\n' "$*" >&2
    exit 1
}

while (($# > 0)); do
    case "$1" in
        --source-root)
            (($# >= 2)) || fail "--source-root requires a path"
            source_root=$2
            shift 2
            ;;
        --dependency)
            (($# >= 2)) || fail "--dependency requires a value"
            dependency=$2
            shift 2
            ;;
        --version)
            (($# >= 2)) || fail "--version requires a value"
            version=$2
            shift 2
            ;;
        --ref)
            (($# >= 2)) || fail "--ref requires a value"
            repository_ref=$2
            shift 2
            ;;
        *)
            fail "unknown argument: $1"
            ;;
    esac
done

[[ -n "$source_root" ]] || fail "--source-root is required"
[[ -n "$dependency" ]] || fail "--dependency is required"
[[ -n "$version" ]] || fail "--version is required"
[[ -n "$repository_ref" ]] || fail "--ref is required"
[[ "$source_root" == /* ]] || fail "--source-root must be absolute"
[[ -d "$source_root" ]] || fail "source root not found: $source_root"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

safe_id=$(printf '%s-%s-%s' "$dependency" "$version" "$repository_ref" |
    tr -cs 'A-Za-z0-9._-' '-' | sed 's/^-//; s/-$//')
[[ -n "$safe_id" ]] || fail "dependency provenance produced an empty cache key"
run_root="${GRAPH_DEP_CACHE_ROOT:-/tmp/graph-dep-runs}/$safe_id"
provenance_path="$run_root/provenance.json"
mkdir -p "$run_root"

if [[ -s "$provenance_path" ]]; then
    python3 - "$provenance_path" "$dependency" "$version" "$repository_ref" "$source_root" <<'PY' ||
import json
import sys
from pathlib import Path

path, dependency, version, repository_ref, source_root = sys.argv[1:]
actual = json.loads(Path(path).read_text(encoding="utf-8"))
expected = {
    "dependency": dependency,
    "version": version,
    "repository_ref": repository_ref,
    "source_root": str(Path(source_root).resolve()),
}
if actual != expected:
    print("dependency graph provenance mismatch", file=sys.stderr)
    raise SystemExit(1)
PY
        fail "cached provenance does not match the opensrc handoff: $provenance_path"
fi

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ensure_script="$script_dir/../../graphify/scripts/ensure-code-graph.sh"
[[ -x "$ensure_script" ]] || fail "missing Graphify ensure script: $ensure_script"

result=$(
    "$ensure_script" \
        --source-root "$source_root" \
        --output-root "$run_root" \
        --immutable \
        --directed \
        --label "dependency-$safe_id"
)

python3 - "$provenance_path" "$dependency" "$version" "$repository_ref" "$source_root" <<'PY'
import json
import sys
from pathlib import Path

path, dependency, version, repository_ref, source_root = sys.argv[1:]
data = {
    "dependency": dependency,
    "version": version,
    "repository_ref": repository_ref,
    "source_root": str(Path(source_root).resolve()),
}
Path(path).write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY

printf 'GRAPH_DEPENDENCY=%s\n' "$dependency"
printf 'GRAPH_DEP_VERSION=%s\n' "$version"
printf 'GRAPH_DEP_REPOSITORY_REF=%s\n' "$repository_ref"
printf 'GRAPH_DEP_RUN_ROOT=%s\n' "$run_root"
printf '%s\n' "$result"
