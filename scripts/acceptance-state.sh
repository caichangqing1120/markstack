#!/usr/bin/env bash

unset MARKSTACK_SETTINGS_PATH MARKSTACK_RECOVERY_PATH MARKSTACK_SESSION_PATH MARKSTACK_RECENT_PATH

acceptance_run() (
    local state
    state="$(mktemp -d "$work/state.XXXXXX")"
    chmod 700 "$state"
    export MARKSTACK_SETTINGS_PATH="${MARKSTACK_SETTINGS_PATH:-$state/settings.json}"
    export MARKSTACK_RECOVERY_PATH="${MARKSTACK_RECOVERY_PATH:-$state/recovery.json}"
    export MARKSTACK_SESSION_PATH="${MARKSTACK_SESSION_PATH:-$state/session.json}"
    export MARKSTACK_RECENT_PATH="${MARKSTACK_RECENT_PATH:-$state/recent.json}"
    exec "$@"
)
