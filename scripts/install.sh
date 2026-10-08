#!/bin/sh
# Install pig-plugins by copying (never linking) it into PiG's home.
#
#   <home>/pig-plugins/            tracked repository files, including every extension and prompts/
#   <home>/bin/pig-plugins         fused PiG executable built from that copy
#   <agent>/{SYSTEM,APPEND_SYSTEM,AGENTS}.md   copied from local/; a differing existing file is
#                                              kept as <file>.pig-plugins-backup-<timestamp>
#
# <home> is $PIG_HOME or ~/.pig; <agent> is $PIG_CODING_AGENT_DIR or <home>/agent.
# Re-running replaces the copy with the current repository state.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
pig_home=${PIG_HOME:-"$HOME/.pig"}
agent_dir=${PIG_CODING_AGENT_DIR:-"$pig_home/agent"}
dest="$pig_home/pig-plugins"

for tool in git tar; do
    command -v "$tool" >/dev/null 2>&1 || { echo "install: $tool is required on PATH" >&2; exit 1; }
done
[ -d "$root/.git" ] || [ -f "$root/.git" ] || { echo "install: run from a git checkout of pig-plugins" >&2; exit 1; }
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    [ -f "$root/local/$f" ] || { echo "install: missing local/$f" >&2; exit 1; }
done

mkdir -p -- "$pig_home" "$agent_dir" "$pig_home/bin"

# 1. Copy tracked files to a staging directory, then swap it in.
stage=$(mktemp -d "$pig_home/.pig-plugins-stage.XXXXXX")
trap 'rm -rf "$stage"' 0
git -C "$root" ls-files -z | tar -C "$root" --null -T - -cf - | tar -C "$stage" -xf -
# Untracked local prompts are still installed from the working tree.
mkdir -p "$stage/local"
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do cp -- "$root/local/$f" "$stage/local/$f"; done
rm -rf -- "$dest.old"
[ ! -e "$dest" ] || mv -- "$dest" "$dest.old"
mv -- "$stage" "$dest"
rm -rf -- "$dest.old"
chmod 755 "$dest"
echo "install: copied repository to $dest"

# 2. Copy the prompts into the agent directory.
stamp=$(date +%s)
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    target="$agent_dir/$f"
    if [ -e "$target" ] && ! cmp -s "$dest/local/$f" "$target"; then
        cp -- "$target" "$target.pig-plugins-backup-$stamp"
        echo "install: backed up $target"
    fi
    cp -- "$dest/local/$f" "$target.tmp.$$"
    mv -- "$target.tmp.$$" "$target"
    echo "install: wrote $target"
done

# 3. Build the fused executable. The builder needs the git checkout; its inputs match the copy above.
binary=$("$root/scripts/dev_build.sh" "$pig_home/bin/pig-plugins")
echo "install: built $binary"
