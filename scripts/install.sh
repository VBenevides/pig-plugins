#!/bin/sh
# Install pig-plugins by copying (never linking) it into PiG's home.
#
#   <home>/pig-plugins/            tracked repository files, including every extension and prompts/
#   <home>/bin/pig-plugins         fused PiG executable built from that copy
#   <agent>/{SYSTEM,APPEND_SYSTEM,AGENTS}.md   copied from prompts/agent/; a differing existing file is
#                                              kept as <file>.pig-plugins-backup-<timestamp>
#   <agent>/skills/<name>/         copied from skills/ (every directory holding a SKILL.md); a differing
#                                  existing skill moves to <agent>/skills-backup/<timestamp>/<name>
#
# <home> is $PIG_HOME or ~/.pig; <agent> is $PIG_CODING_AGENT_DIR or <home>/agent.
# Re-running replaces the copy with the current repository state.
set -eu

# Piped from curl (`curl ... | sh`) there is no checkout beside the script: clone one and run its copy.
case "$0" in
    */*) here=$(dirname -- "$0") ;;
    *) here=. ;;
esac
if [ ! -f "$here/dev_build.sh" ]; then
    command -v git >/dev/null 2>&1 || { echo "install: git is required on PATH" >&2; exit 1; }
    checkout=$(mktemp -d "${TMPDIR:-/tmp}/pig-plugins-install.XXXXXX")
    trap 'rm -rf "$checkout"' 0
    git clone --quiet --depth 1 "${PIG_PLUGINS_REPO:-https://github.com/VBenevides/pig-plugins}" "$checkout/src"
    "$checkout/src/scripts/install.sh"
    exit 0
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
pig_home=${PIG_HOME:-"$HOME/.pig"}
agent_dir=${PIG_CODING_AGENT_DIR:-"$pig_home/agent"}
dest="$pig_home/pig-plugins"

for tool in git tar; do
    command -v "$tool" >/dev/null 2>&1 || { echo "install: $tool is required on PATH" >&2; exit 1; }
done
[ -d "$root/.git" ] || [ -f "$root/.git" ] || { echo "install: run from a git checkout of pig-plugins" >&2; exit 1; }
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    [ -f "$root/prompts/agent/$f" ] || { echo "install: missing prompts/agent/$f" >&2; exit 1; }
done

mkdir -p -- "$pig_home" "$agent_dir" "$pig_home/bin"

# 1. Copy tracked files to a staging directory, then swap it in.
stage=$(mktemp -d "$pig_home/.pig-plugins-stage.XXXXXX")
trap 'rm -rf "$stage"' 0
git -C "$root" ls-files -z | tar -C "$root" --null -T - -cf - | tar -C "$stage" -xf -
# Skills are copied from the working tree, so untracked ones are installed too.
if [ -d "$root/skills" ]; then
    rm -rf -- "$stage/skills"
    cp -R -- "$root/skills" "$stage/skills"
fi
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
    if [ -e "$target" ] && ! cmp -s "$dest/prompts/agent/$f" "$target"; then
        cp -- "$target" "$target.pig-plugins-backup-$stamp"
        echo "install: backed up $target"
    fi
    cp -- "$dest/prompts/agent/$f" "$target.tmp.$$"
    mv -- "$target.tmp.$$" "$target"
    echo "install: wrote $target"
done

# 3. Copy the skills into the agent directory.
for skill in "$dest"/skills/*/; do
    [ -f "$skill/SKILL.md" ] || continue
    name=$(basename -- "$skill")
    target="$agent_dir/skills/$name"
    mkdir -p -- "$agent_dir/skills"
    if [ -e "$target" ] && ! diff -rq -- "$skill" "$target" >/dev/null 2>&1; then
        # Backups stay outside skills/ so PiG does not discover them as duplicate skills.
        mkdir -p -- "$agent_dir/skills-backup/$stamp"
        mv -- "$target" "$agent_dir/skills-backup/$stamp/$name"
        echo "install: backed up $target"
    fi
    if [ ! -e "$target" ]; then
        cp -R -- "$skill" "$target.tmp.$$"
        mv -- "$target.tmp.$$" "$target"
        echo "install: wrote $target"
    fi
done

# 4. Build the fused executable. The builder needs the git checkout; its inputs match the copy above.
binary=$("$root/scripts/dev_build.sh" "$pig_home/bin/pig-plugins")
echo "install: built $binary"
