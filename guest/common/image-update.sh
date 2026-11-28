#!/bin/sh
set -eu
export PATH="$HOME/.local/bin:$PATH"
claude update
mise exec -- npm install --global --prefix "$HOME/.local" @openai/codex
