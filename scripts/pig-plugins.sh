#!/bin/sh
# Installed entry point; the native executable remains independently usable.
set -eu
pig_home=${PIG_HOME:-"$HOME/.pig"}
if [ "${1:-}" = --update ]; then
    [ "$#" -eq 1 ] || { echo 'usage: pig-plugins --update' >&2; exit 2; }
    command -v curl >/dev/null 2>&1 || { echo 'update: curl is required' >&2; exit 1; }
    installer=$(mktemp "${TMPDIR:-/tmp}/pig-plugins-update.XXXXXX")
    trap 'rm -f "$installer"' 0
    # Download completely before executing; a failed/truncated transfer never runs.
    curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 10 --max-time 60 \
        https://raw.githubusercontent.com/VBenevides/pig-plugins/main/scripts/install.sh -o "$installer"
    PIG_PLUGINS_UPDATE=1 sh "$installer"
    exit 0
fi
native="$pig_home/bin/pig-plugins-native"
[ -x "$native" ] || { echo "pig-plugins: missing native executable: $native" >&2; exit 1; }
PIG_PLUGINS_HOST_VERSION=$("$native" --version)
export PIG_PLUGINS_HOST_VERSION
exec "$native" "$@"
