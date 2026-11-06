#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
template="$script_dir/hitl-loop.template.sh"
output="$(printf '\ny\nexample error\n' | bash "$template")"

grep -Fq 'SYMPTOM_PRESENT=y' <<< "$output"
grep -Fq 'OBSERVATION=example error' <<< "$output"

printf 'PASS hitl-loop.template.sh captures the manual reproduction signal\n'
