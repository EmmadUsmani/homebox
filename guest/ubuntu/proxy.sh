#!/bin/sh
set -eu

admin=${HOMEBOX_USER:-${SUDO_USER:?admin user required}}

# /etc/environment is read by SSH's PAM session. Preserve unrelated entries.
temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT
sed '/^# BEGIN HOMEBOX PROXY$/,/^# END HOMEBOX PROXY$/d' /etc/environment > "$temporary"
apt_config=/etc/apt/apt.conf.d/80-homebox-proxy
docker_dropin=/etc/systemd/system/docker.service.d/80-homebox-proxy.conf

rm -f "$apt_config"
install -m 0644 "$temporary" /etc/environment

# Remove legacy proxy injection from containers.
sudo -H -u "$admin" python3 - <<'PY'
import json
import sys
from pathlib import Path
from tempfile import NamedTemporaryFile

path = Path.home() / ".docker" / "config.json"
if not path.exists():
    sys.exit(0)

config = json.loads(path.read_text())
proxies = config.get("proxies", {})
defaults = proxies.get("default", {})
keys = ("httpProxy", "httpsProxy", "noProxy")
if not any(key in defaults for key in keys):
    sys.exit(0)
for key in keys:
    defaults.pop(key, None)
if not defaults:
    proxies.pop("default")
if not proxies:
    config.pop("proxies")

if config:
    with NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as temporary:
        temporary.write(json.dumps(config, indent=2) + "\n")
    Path(temporary.name).replace(path)
else:
    path.unlink(missing_ok=True)
PY

if [ -f "$docker_dropin" ]; then
    rm -f "$docker_dropin"
    systemctl daemon-reload
    if systemctl is-active --quiet docker.service; then
        systemctl reset-failed docker.service
        systemctl restart docker.service
    fi
fi
