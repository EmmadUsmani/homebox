#!/bin/sh
set -eu

if [ ! -x "$HOME/.local/bin/claude" ]; then
    installer=$(mktemp)
    trap 'rm -f "$installer"' EXIT
    curl -fsSL https://claude.ai/install.sh -o "$installer"
    bash "$installer" stable
fi
if [ ! -x "$HOME/.local/bin/codex" ]; then
    "$HOME/.local/bin/mise" exec -- npm install --global --prefix "$HOME/.local" @openai/codex
fi
