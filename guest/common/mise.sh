#!/bin/sh
set -eu

mise="$HOME/.local/bin/mise"
if [ ! -x "$mise" ]; then
    installer=$(mktemp)
    trap 'rm -f "$installer"' EXIT
    curl -fsSL https://mise.run -o "$installer"
    MISE_INSTALL_PATH="$mise" sh "$installer"
fi
"$mise" use --global node@24 python@3.14

# Keep the expressions literal until shell startup.
# shellcheck disable=SC2016
profile_line='if [ -x "$HOME/.local/bin/mise" ]; then export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"; fi'
# shellcheck disable=SC2016
bashrc_line='if [ -x "$HOME/.local/bin/mise" ]; then eval "$("$HOME/.local/bin/mise" activate bash)"; fi'
grep -Fqx "$profile_line" "$HOME/.profile" 2>/dev/null ||
    printf '\n%s\n' "$profile_line" >> "$HOME/.profile"
grep -Fqx "$bashrc_line" "$HOME/.bashrc" 2>/dev/null ||
    printf '\n%s\n' "$bashrc_line" >> "$HOME/.bashrc"
