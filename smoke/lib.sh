# shellcheck shell=sh
# Shared helpers for smoke tests. Source it from an area's test.sh:
#   . ../lib.sh
#   check 'Node runs' node -e 'process.exit(0)'
#   finish
# Each check prints PASS, FAIL or SKIP and a label; failures show the last
# lines of output. finish exits 1 if any check failed.
smoke_failures=0

pass() { printf 'PASS %s\n' "$*"; }
skip() { printf 'SKIP %s\n' "$*"; }
fail() {
    printf 'FAIL %s\n' "$*"
    smoke_failures=$((smoke_failures + 1))
}

# full_only check LABEL COMMAND [ARG...]
full_only() {
    if [ "${SMOKE_LEVEL:-full}" = full ]; then
        "$@"
    fi
}

smoke_matches() {
    [ -z "${SMOKE_MATCH:-}" ] || printf '%s\n' "$1" | grep -Eiq -- "$SMOKE_MATCH"
}

# Image manifest rows default to full. A quick row stages only its native image.
smoke_image_enabled() {
    [ "${SMOKE_LEVEL:-full}" = full ] || [ "$1" = quick ]
}

# check LABEL COMMAND [ARG...]
check() {
    smoke_label=$1
    shift
    smoke_matches "$smoke_label" || return 0
    if smoke_output=$("$@" 2>&1); then
        pass "$smoke_label"
    else
        fail "$smoke_label"
        printf '%s\n' "$smoke_output" | tail -n 20 | sed 's/^/    /'
    fi
}

# check_sh LABEL 'shell snippet'
check_sh() {
    check "$1" bash -c "$2"
}

finish() {
    if [ "$smoke_failures" -ne 0 ]; then
        printf '%s check(s) failed\n' "$smoke_failures"
        exit 1
    fi
}
