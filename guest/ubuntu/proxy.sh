#!/bin/sh
set -eu

proxy=${HOMEBOX_PROXY_URL:-}
bypass=${HOMEBOX_NO_PROXY:-localhost,127.0.0.1,::1}
admin=${HOMEBOX_USER:-${SUDO_USER:?admin user required}}
case "$proxy$bypass" in
    *[[:space:]]*|*'"'*|*\\*) echo "Invalid proxy or no-proxy value" >&2; exit 1 ;;
esac

# /etc/environment is read by SSH's PAM session. Preserve unrelated entries.
temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT
sed '/^# BEGIN HOMEBOX PROXY$/,/^# END HOMEBOX PROXY$/d' /etc/environment > "$temporary"
apt_config=/etc/apt/apt.conf.d/80-homebox-proxy
docker_dropin=/etc/systemd/system/docker.service.d/80-homebox-proxy.conf

if [ -n "$proxy" ]; then
    {
        echo '# BEGIN HOMEBOX PROXY'
        for key in http_proxy https_proxy HTTP_PROXY HTTPS_PROXY; do
            printf '%s="%s"\n' "$key" "$proxy"
        done
        printf 'no_proxy="%s"\nNO_PROXY="%s"\n' "$bypass" "$bypass"
        echo '# END HOMEBOX PROXY'
    } >> "$temporary"
    printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "%s";\n' "$proxy" "$proxy" > "$apt_config"
else
    rm -f "$apt_config"
fi
install -m 0644 "$temporary" /etc/environment

# Merge the managed Docker client keys without replacing unrelated settings.
sudo -H -u "$admin" python3 - "$proxy" "$bypass" <<'PY'
import json
import sys
from pathlib import Path
from tempfile import NamedTemporaryFile

proxy, bypass = sys.argv[1:]
path = Path.home() / ".docker" / "config.json"
if not proxy and not path.exists():
    sys.exit(0)

config = json.loads(path.read_text()) if path.exists() else {}
proxies = config.setdefault("proxies", {})
defaults = proxies.setdefault("default", {})
for key, value in (("httpProxy", proxy), ("httpsProxy", proxy), ("noProxy", bypass)):
    if proxy:
        defaults[key] = value
    else:
        defaults.pop(key, None)
if not defaults:
    proxies.pop("default")
if not proxies:
    config.pop("proxies")

if config:
    path.parent.mkdir(mode=0o700, exist_ok=True)
    with NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as temporary:
        temporary.write(json.dumps(config, indent=2) + "\n")
    Path(temporary.name).replace(path)
else:
    path.unlink(missing_ok=True)
PY

: > "$temporary"
if [ -n "$proxy" ]; then
    # systemd interprets percent escapes in Environment= values.
    escaped_proxy=$(printf '%s' "$proxy" | sed 's/%/%%/g')
    escaped_bypass=$(printf '%s' "$bypass" | sed 's/%/%%/g')
    printf '[Service]\nEnvironment="HTTP_PROXY=%s" "HTTPS_PROXY=%s" "NO_PROXY=%s"\n' \
        "$escaped_proxy" "$escaped_proxy" "$escaped_bypass" > "$temporary"
fi
if [ -n "$proxy" ] && ! cmp -s "$temporary" "$docker_dropin"; then
    install -D -m 0644 "$temporary" "$docker_dropin"
    docker_changed=1
elif [ -z "$proxy" ] && [ -f "$docker_dropin" ]; then
    rm -f "$docker_dropin"
    docker_changed=1
fi
if [ "${docker_changed:-0}" = 1 ]; then
    systemctl daemon-reload
    if systemctl is-active --quiet docker.service; then
        # Explicit proxy changes can restart Docker several times in a row.
        systemctl reset-failed docker.service
        systemctl restart docker.service
    fi
fi
