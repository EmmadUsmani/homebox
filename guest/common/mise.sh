#!/bin/sh
set -eu

mise="$HOME/.local/bin/mise"
if [ ! -x "$mise" ]; then
    installer=$(mktemp)
    trap 'rm -f "$installer"' EXIT
    curl -fsSL https://mise.run -o "$installer"
    MISE_INSTALL_PATH="$mise" sh "$installer"
fi
"$mise" use --global node@24 python@3.14 uv

# Keep the expressions literal until shell startup.
# shellcheck disable=SC2016
profile_line='if [ -x "$HOME/.local/bin/mise" ]; then export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:$PATH"; fi'
# shellcheck disable=SC2016
bashrc_line='if [ -x "$HOME/.local/bin/mise" ]; then eval "$("$HOME/.local/bin/mise" activate bash)"; fi'
grep -Fqx "$profile_line" "$HOME/.profile" 2>/dev/null ||
    printf '\n%s\n' "$profile_line" >> "$HOME/.profile"
# bash also reads ~/.bashrc for `ssh host cmd`, but Ubuntu's ~/.bashrc returns
# early when the shell isn't interactive, so the PATH has to come first.
if ! grep -Fqx "$profile_line" "$HOME/.bashrc" 2>/dev/null; then
    bashrc=$(mktemp)
    { printf '%s\n' "$profile_line"; cat "$HOME/.bashrc" 2>/dev/null || :; } > "$bashrc"
    cat "$bashrc" > "$HOME/.bashrc"
    rm -f "$bashrc"
fi
grep -Fqx "$bashrc_line" "$HOME/.bashrc" 2>/dev/null ||
    printf '\n%s\n' "$bashrc_line" >> "$HOME/.bashrc"
