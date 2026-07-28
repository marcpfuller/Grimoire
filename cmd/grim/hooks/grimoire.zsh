# grimoire.zsh — Grimoire zsh shell integration
#
# Source this from your .zshrc AFTER your theme (e.g. oh-my-zsh):
#
#   [[ -f /path/to/grimoire.zsh ]] && source /path/to/grimoire.zsh
#
# Environment variables (set before sourcing to override defaults):
#
#   GRIMOIRE_DAEMON_ADDR   Daemon base URL (default: inherited from grim shell)
#   GRIMOIRE_NO_RPROMPT=1  Disable the [short_id] right-prompt segment
#   GRIMOIRE_CURL_TIMEOUT  curl max-time in seconds (default: 2)
#
# The script is a no-op if GRIMOIRE_PANE_ID is not set, so sourcing it
# unconditionally in .zshrc is safe — it only activates inside `grim shell`.

# --- Guard: only activate inside a grim shell session ---
[[ -z "${GRIMOIRE_PANE_ID}" ]] && return 0

# --- Dependencies ---
if ! command -v curl >/dev/null 2>&1; then
    print -u2 "[grimoire] curl not found — shell integration disabled"
    return 1
fi
if ! command -v jq >/dev/null 2>&1; then
    print -u2 "[grimoire] jq not found — shell integration disabled"
    return 1
fi

# --- Configuration ---
: "${GRIMOIRE_DAEMON_ADDR:=http://127.0.0.1:7777}"
: "${GRIMOIRE_CURL_TIMEOUT:=2}"

# Load the bearer token from the token file. Reading from the file (rather
# than an exported env var) means child processes spawned inside the session
# never inherit the token — only this shell process has it.
_grimoire_tok=""
if [[ -r "${HOME}/.grim/token" ]]; then
    _grimoire_tok=$(tr -d '[:space:]' < "${HOME}/.grim/token" 2>/dev/null)
fi

# Internal state — do not set these manually.
_grimoire_next_short_id=""   # short_id reserved for the next command
_grimoire_active_short_id="" # short_id of the currently running command
_grimoire_active_started_at="" # started_at ms of the currently running command
_grimoire_preexec_fired=0    # 1 if preexec ran since the last precmd; 0 on empty Enter

# --- Millisecond timestamp ---
# python3 is universally available and gives true ms precision.
# Falls back to GNU date (+%s%3N) then POSIX date (second granularity).
_grimoire_ms() {
    if command -v python3 >/dev/null 2>&1; then
        python3 -c "import time; print(int(time.time()*1000))"
    elif date +%s%3N 2>/dev/null | grep -qE '^[0-9]{13}$'; then
        date +%s%3N
    else
        printf '%d000' "$(date +%s)"
    fi
}

# --- Reserve the short ID for the next command ---
# Called at the end of precmd so the ID is ready before the next preexec.
# Silently clears _grimoire_next_short_id on failure so preexec knows to skip.
_grimoire_reserve_next_id() {
    local response
    response=$(curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
        -H "Authorization: Bearer ${_grimoire_tok}" \
        "${GRIMOIRE_DAEMON_ADDR}/api/next-id?pane=${GRIMOIRE_PANE_ID}" \
        2>/dev/null) || { _grimoire_next_short_id=""; return; }

    _grimoire_next_short_id=$(printf '%s' "${response}" \
        | jq -r '.short_id // empty' 2>/dev/null)
}

# --- preexec: fires before each command runs ---
# Receives the raw command string as $1.
# Calls POST /api/execute, then emits the enriched OSC activate marker so
# pty.Session.readLoop begins capturing output.
_grimoire_preexec() {
    local cmd="${1}"

    # Mark that a real command is running so precmd reserves a fresh ID.
    _grimoire_preexec_fired=1

    # No reservation means daemon wasn't reachable when the prompt was drawn.
    # Skip silently so the command still runs normally.
    [[ -z "${_grimoire_next_short_id}" ]] && return

    local started_at
    started_at=$(_grimoire_ms)

    local response
    response=$(curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
        -X POST "${GRIMOIRE_DAEMON_ADDR}/api/execute" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${_grimoire_tok}" \
        -d "{\"pane_id\":\"${GRIMOIRE_PANE_ID}\",\"command\":$(printf '%s' "${cmd}" | jq -Rs .),\"started_at\":${started_at}}" \
        2>/dev/null) || return

    local command_id short_id
    command_id=$(printf '%s' "${response}" | jq -r '.command_id // empty' 2>/dev/null)
    short_id=$(printf '%s' "${response}" | jq -r '.short_id // empty' 2>/dev/null)

    [[ -z "${command_id}" || -z "${short_id}" ]] && return

    _grimoire_active_short_id="${short_id}"
    _grimoire_active_started_at="${started_at}"

    # Emit the enriched OSC activate marker.
    # pty.Session.readLoop parses id/start/sid and calls state.activate()
    # without any in-process HTTP callback.
    printf '\033]133;C;id=%s;start=%s;sid=%s\033\\' \
        "${command_id}" "${started_at}" "${short_id}"
}

# --- precmd: fires before each prompt is drawn ---
# Finalises the previous command (if any) and reserves the short ID for the next.
_grimoire_precmd() {
    local exit_code=$? # must be first — captures the command's exit status

    # Capture and reset the fired flag. If preexec didn't fire (empty Enter),
    # we keep the existing short_id rather than reserving a new one.
    local preexec_fired=${_grimoire_preexec_fired}
    _grimoire_preexec_fired=0

    if [[ -n "${_grimoire_active_short_id}" ]]; then
        local ended_at
        ended_at=$(_grimoire_ms)

        # Best-effort finalise. Errors are non-fatal — the command is already done.
        curl -sf --max-time "${GRIMOIRE_CURL_TIMEOUT}" \
            -X POST "${GRIMOIRE_DAEMON_ADDR}/api/precmd" \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${_grimoire_tok}" \
            -d "{\"pane_id\":\"${GRIMOIRE_PANE_ID}\",\"short_id\":\"${_grimoire_active_short_id}\",\"ended_at\":${ended_at},\"exit_code\":${exit_code}}" \
            2>/dev/null || true

        # Always emit the deactivate marker — prevents pty.Session from staying
        # stuck in Capturing state if the HTTP call above failed.
        printf '\033]133;D\033\\'

        _grimoire_active_short_id=""
        _grimoire_active_started_at=""
    fi

    # Only reserve a new ID when a command was actually executed, or on the
    # very first prompt where no ID has been reserved yet.
    if [[ ${preexec_fired} -eq 1 || -z "${_grimoire_next_short_id}" ]]; then
        _grimoire_reserve_next_id
    fi
}

# --- Prompt integration ---
# Sets RPROMPT to show [short_id] in yellow.
# Disable with GRIMOIRE_NO_RPROMPT=1 and use ${_grimoire_next_short_id} in your
# own PROMPT/RPROMPT instead.
if [[ -z "${GRIMOIRE_NO_RPROMPT}" ]]; then
    _grimoire_rprompt_base="${RPROMPT}"

    _grimoire_update_rprompt() {
        if [[ -n "${_grimoire_next_short_id}" ]]; then
            RPROMPT="%F{yellow}[${_grimoire_next_short_id}]%f${_grimoire_rprompt_base:+ ${_grimoire_rprompt_base}}"
        else
            RPROMPT="${_grimoire_rprompt_base}"
        fi
    }
fi

# --- Register hooks ---
autoload -Uz add-zsh-hook
add-zsh-hook preexec _grimoire_preexec
add-zsh-hook precmd  _grimoire_precmd
[[ -z "${GRIMOIRE_NO_RPROMPT}" ]] && add-zsh-hook precmd _grimoire_update_rprompt
