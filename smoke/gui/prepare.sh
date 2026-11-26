#!/bin/sh
set -eu

"${BOX:?}" desktop "${SMOKE_VM:?}" --install
