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

# check LABEL COMMAND [ARG...]
check() {
    smoke_label=$1
    shift
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
