#!/bin/sh
# Run one JavaScript command from an Ouro quality snapshot.
#
# The snapshot omits ignored files, so node_modules is missing there. Install the locked dev dependencies with
# `npm ci` (no lifecycle scripts, cache preferred), then run the command unchanged.
#
# Usage: scripts/ouro-js.sh npm test
set -eu
[ "$#" -gt 0 ] || { echo "usage: scripts/ouro-js.sh COMMAND [ARGUMENT...]" >&2; exit 2; }
npm ci --ignore-scripts --prefer-offline --no-audit --no-fund >&2
exec "$@"
