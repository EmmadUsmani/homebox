#!/bin/sh
export LC_ALL=C
awk 'NR == 1 { print "uptime", $1 }' /proc/uptime
awk 'NR == 1 { print "load", $1, $2, $3 }' /proc/loadavg
awk '
    $1 == "MemTotal:" { total=$2 }
    $1 == "MemAvailable:" { available=$2 }
    END { if (total && available != "") print "memory", total, available }
' /proc/meminfo
df -Pk / | awk 'NR == 2 { print "disk", $2, $3 }'

[ "${HOMEBOX_STATUS_MODE:-}" = detail ] || exit 0

ps -eo pid=,pcpu=,rss=,comm= --sort=-pcpu |
    head -n 10 | awk '{ print "process", $1, $2, $3, $4 }'
if command -v ss >/dev/null 2>&1; then
    ss -Hltnp | head -n 200 | awk '{ address=$4; $1=$2=$3=$4=$5=""; sub(/^ +/, ""); print "listener", address, $0 }'
fi
if command -v docker >/dev/null 2>&1; then
    docker ps --format '{{.ID}} {{.Names}} {{.Image}} {{.Status}}' | head -n 100 |
        sed 's/^/container /'
fi
ps -eo pid=,comm= | awk '$2 == "claude" || $2 == "codex" { print $1, $2 }' |
    head -n 100 | while read -r pid agent; do
        cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null | tr '\r\n' ' ')
        printf 'agent %s %s %s\n' "$pid" "$agent" "${cwd% }"
    done
exit 0
