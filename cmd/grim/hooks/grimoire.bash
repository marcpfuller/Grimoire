# grimoire.bash — Grimoire bash shell integration
#
# Source this from your .bashrc AFTER your prompt setup:
#
#   [[ -f /path/to/grimoire.bash ]] && source /path/to/grimoire.bash
#
# Environment variables (set before sourcing to override defaults):
#
#   GRIMOIRE_DAEMON_ADDR   Daemon base URL (default: inherited from grim shell)
#   GRIMOIRE_NO_PS1=1      Disable the [short_id] PS1 prefix
#   GRIMOIRE_CURL_TIMEOUT  curl max-time in seconds (default: 2)
#
# The script is a no-op if GRIMOIRE_PANE_ID is not set, so sourcing it
# unconditionally in .bashrc is safe — it only activates inside `grim shell`.

# --- Guard: only activate inside a grim shell session ---
[[ -z "${GRIMOIRE_PANE_ID}" ]] && return 0

# --- Dependencies ---
if ! command -v curl >/dev/null 2>&1; then
    echo "[grimoire] curl not found — shell integration disabled" >&2
    return 1
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "[grimoire] jq not found — shell integration disabled" >&2
    return 1
fi

# --- Configuration ---
: "${GRIMOIRE_DAEMON_ADDR:=http://127.0.0.1:7777}"
: "${GRIMOIRE_CURL_TIMEOUT:=2}"

# Internal state — do not set these manually.
_grimoire_next_short_id=""
_grimoire_active_short_id=""
_grimoire_active_started_at=""
_grimoire_preexec_done=""  # guards against DEBUG trap re-entrancy
_grimoire_preexec_fired="" # set to 1 when a real command ran; empty on empty Enter

# --- Millisecond timestamp ---
_grimoire_ms() {
    if command -v python3 >/dev/null 2>&1; then
        python3 -c "import time; print(int(time.time()*1000))"
    elif date +%s%3N 2>/dev/null | grep -qE '^[0-9]{13}$'; then
        date +%s%3N
    else
        printf '%d' $(( $(date +%s) * 1000 ))
    fi
}

# --- Reserve the short ID for the next command ---
_grimoire_reserve_next_id() {
    local response
    response=$(curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
        "${GRIMOIRE_DAEMON_ADDR}/api/next-id?pane=${GRIMOIRE_PANE_ID}" \
        2>/dev/null) || { _grimoire_next_short_id=""; return; }

    _grimoire_next_short_id=$(printf '%s' "${response}" \
        | jq -r '.short_id // empty' 2>/dev/null)
}

# --- preexec: called via DEBUG trap before each interactive command ---
# The DEBUG trap fires before every simple command, including those inside
# PROMPT_COMMAND and this function itself. The re-entrancy guard prevents
# recursive calls. $BASH_COMMAND holds the command about to run.
_grimoire_preexec_trap() {
    # Re-entrancy guard: skip if we're already inside a grimoire function,
    # inside PROMPT_COMMAND, or the user just hit enter on an empty line.
    [[ -n "${_grimoire_preexec_done}" ]] && return
    [[ "${BASH_COMMAND}" == _grimoire_* ]] && return
    [[ "${BASH_COMMAND}" == __grimoire_* ]] && return

    # COMP_LINE is set during completion — don't capture those pseudo-commands.
    [[ -n "${COMP_LINE}" ]] && return

    # Only fire for interactive commands (not subshells spawned by the prompt).
    [[ "${BASH_COMMAND}" == "command_not_found_handle"* ]] && return

    _grimoire_preexec_done=1
    local cmd="${BASH_COMMAND}"

    # Mark that a real command is running so precmd reserves a fresh ID.
    _grimoire_preexec_fired=1

    [[ -z "${_grimoire_next_short_id}" ]] && return

    local started_at
    started_at=$(_grimoire_ms)

    local response
    response=$(curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
        -X POST "${GRIMOIRE_DAEMON_ADDR}/api/execute" \
        -H "Content-Type: application/json" \
        -d "{\"pane_id\":\"${GRIMOIRE_PANE_ID}\",\"command\":$(printf '%s' "${cmd}" | jq -Rs .),\"started_at\":${started_at}}" \
        2>/dev/null) || return

    local command_id short_id
    command_id=$(printf '%s' "${response}" | jq -r '.command_id // empty' 2>/dev/null)
    short_id=$(printf '%s' "${response}" | jq -r '.short_id // empty' 2>/dev/null)

    [[ -z "${command_id}" || -z "${short_id}" ]] && return

    _grimoire_active_short_id="${short_id}"
    _grimoire_active_started_at="${started_at}"

    # Emit enriched OSC activate marker — pty.Session.readLoop parses this.
    printf '\033]133;C;id=%s;start=%s;sid=%s\033\\' \
        "${command_id}" "${started_at}" "${short_id}"
}

# --- precmd: runs as part of PROMPT_COMMAND ---
_grimoire_precmd() {
    local exit_code=$? # must be first
    _grimoire_preexec_done="" # reset re-entrancy guard for next command

    # Capture and reset the fired flag. If preexec didn't fire (empty Enter),
    # keep the existing short_id rather than reserving a new one.
    local preexec_fired="${_grimoire_preexec_fired}"
    _grimoire_preexec_fired=""

    if [[ -n "${_grimoire_active_short_id}" ]]; then
        local ended_at
        ended_at=$(_grimoire_ms)

        curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
            -X POST "${GRIMOIRE_DAEMON_ADDR}/api/precmd" \
            -H "Content-Type: application/json" \
            -d "{\"pane_id\":\"${GRIMOIRE_PANE_ID}\",\"short_id\":\"${_grimoire_active_short_id}\",\"ended_at\":${ended_at},\"exit_code\":${exit_code}}" \
            2>/dev/null || true

        # Always emit deactivate — prevents pty.Session stuck in Capturing state.
        printf '\033]133;D\033\\'

        _grimoire_active_short_id=""
        _grimoire_active_started_at=""
    fi

    # Only reserve a new ID when a command ran, or on the very first prompt.
    if [[ -n "${preexec_fired}" || -z "${_grimoire_next_short_id}" ]]; then
        _grimoire_reserve_next_id
    fi

    # Update PS1 prefix with new short_id.
    if [[ -z "${GRIMOIRE_NO_PS1}" ]]; then
        if [[ -n "${_grimoire_next_short_id}" ]]; then
            PS1="\[\e[33m\][${_grimoire_next_short_id}]\[\e[0m\] ${_grimoire_ps1_base}"
        else
            PS1="${_grimoire_ps1_base}"
        fi
    fi
}

# --- Prompt integration ---
if [[ -z "${GRIMOIRE_NO_PS1}" ]]; then
    # Snapshot the current PS1 before we prepend to it.
    _grimoire_ps1_base="${PS1}"
fi

# --- Register hooks ---
# Prepend _grimoire_precmd to PROMPT_COMMAND so exit_code=$? is captured first.
if [[ -z "${PROMPT_COMMAND}" ]]; then
    PROMPT_COMMAND="_grimoire_precmd"
else
    PROMPT_COMMAND="_grimoire_precmd;${PROMPT_COMMAND}"
fi

trap '_grimoire_preexec_trap' DEBUG

# Inherit errexit/pipefail from the parent shell without disrupting the hooks.
# The DEBUG trap must survive subshells; this ensures it does.
export _grimoire_trap_inherited=1
