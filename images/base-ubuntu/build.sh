#!/bin/sh
set -eu
exec "${BOX:-box}" image rebuild --profile ubuntu "$@"
