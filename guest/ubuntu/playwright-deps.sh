#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
# Chromium libraries for Ubuntu 26.04 from Playwright's nativeDeps.ts.
apt-get install -y --no-install-recommends \
    xvfb xauth fonts-liberation fonts-noto-color-emoji \
    libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 \
    libcairo2 libcups2t64 libdbus-1-3 libdrm2 libgbm1 libglib2.0-0t64 \
    libnspr4 libnss3 libpango-1.0-0 libx11-6 libxcb1 libxcomposite1 \
    libxdamage1 libxext6 libxfixes3 libxkbcommon0 libxrandr2
