#!/bin/sh
# Run one Go command the way a developer does, from an Ouro quality snapshot.
#
# The snapshot lacks the ignored go.work and uses a temporary HOME, so plain `go vet ./...` cannot resolve the
# patched PiG SDK. This wrapper restores the real HOME (module and build caches), generates go.work with
# scripts/gowork.sh, and runs the command unchanged. RPC tests start a real `pig`; they use the fused binary in
# PIG_FUSED_TEST_BINARY, or ~/.pig/bin/pig-plugins when that file is executable.
#
# Usage: scripts/ouro-go.sh go vet ./...
set -eu
[ "$#" -gt 0 ] || { echo "usage: scripts/ouro-go.sh COMMAND [ARGUMENT...]" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
user_home=$(getent passwd "$(id -u)" | cut -d: -f6)
[ -n "$user_home" ] && [ -d "$user_home" ] || { echo "ouro-go: cannot find the home directory" >&2; exit 1; }
HOME=$user_home
export HOME
if [ -z "${PIG_FUSED_TEST_BINARY:-}" ] && [ -x "$HOME/.pig/bin/pig-plugins" ]; then
    PIG_FUSED_TEST_BINARY="$HOME/.pig/bin/pig-plugins"
    export PIG_FUSED_TEST_BINARY
fi
cd "$root"
./scripts/gowork.sh >&2
exec "$@"
