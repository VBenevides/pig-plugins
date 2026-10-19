#!/bin/sh
# Compatibility entry point for source-tree callers; installs use the binary directly.
set -eu
pig_home=${PIG_HOME:-"$HOME/.pig"}
# Keep an existing system ast-grep first. Fall back to the managed dependency.
PATH="$PATH:$pig_home/bin"
export PATH
exec "$pig_home/bin/pig-plugins" "$@"
