#!/usr/bin/env bash
# Human-in-the-loop reproduction template.
# Copy this file to a temporary debug location, edit only the marked section,
# and run it when the symptom cannot be reproduced unattended.
#
# Record observations only. Never enter credentials, tokens, authorization
# headers, private data, or complete sensitive payloads.

set -euo pipefail

step() {
  printf '\n>>> %s\n' "$1"
  read -r -p '    [Enter when done] ' _
}

capture() {
  local variable_name="$1"
  local question="$2"
  local answer

  printf '\n>>> %s\n' "$question"
  read -r -p '    > ' answer
  printf -v "$variable_name" '%s' "$answer"
}

# --- edit below ---------------------------------------------------------

step 'Perform the manual action that reaches the suspected bug path.'

capture SYMPTOM_PRESENT 'Did the exact reported symptom occur? (y/n)'

capture OBSERVATION 'Enter the minimal redacted error or observation:'

# --- edit above ---------------------------------------------------------

printf '\n--- Captured ---\n'
printf 'SYMPTOM_PRESENT=%s\n' "$SYMPTOM_PRESENT"
printf 'OBSERVATION=%s\n' "$OBSERVATION"
