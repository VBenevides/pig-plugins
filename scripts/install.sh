#!/bin/sh
# Install pig-plugins by copying (never linking) it into PiG's home.
# Installs the compatible PiG release (override with PIG_VERSION) when `pig` is not on PATH, then builds the
# fused executable; prompts and skills are installed only after that build succeeds.
#
#   <home>/pig-plugins/            tracked repository files, including every extension and prompts/
#   <home>/bin/pig-plugins         fused PiG executable with distribution update support
#   <agent>/{SYSTEM,APPEND_SYSTEM,AGENTS}.md   copied from prompts/agent/; a differing existing file is
#                                              kept as <file>.pig-plugins-backup-<timestamp>
#   <agent>/keybindings.json                   copied from config/agent/ only when absent; never overwritten
#   <agent>/skills/<name>/         copied from skills/ (every directory holding a SKILL.md); a differing
#                                  existing skill moves to <agent>/skills-backup/<timestamp>/<name>
#
# <home> is $PIG_HOME or ~/.pig; <agent> is $PIG_CODING_AGENT_DIR or <home>/agent.
# Re-running replaces the copy with the current repository state.
# Installation stages and build progress are printed to stderr.
# PiG bootstrap output is retained in a temporary log and shown on failure.
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
    echo "install: downloading repository" >&2
    if [ -n "${PIG_PLUGINS_COMMIT:-}" ]; then
        case "$PIG_PLUGINS_COMMIT" in
            *[!0-9a-f]*|'') echo "install: invalid pinned commit" >&2; exit 1 ;;
        esac
        [ "${#PIG_PLUGINS_COMMIT}" -eq 40 ] || { echo "install: invalid pinned commit" >&2; exit 1; }
        git init --quiet "$checkout/src"
        git -C "$checkout/src" remote add origin "${PIG_PLUGINS_REPO:-https://github.com/VBenevides/pig-plugins}"
        git -C "$checkout/src" fetch --quiet --depth 1 origin "$PIG_PLUGINS_COMMIT"
        git -C "$checkout/src" checkout --quiet --detach FETCH_HEAD
    else
        git clone --quiet --depth 1 "${PIG_PLUGINS_REPO:-https://github.com/VBenevides/pig-plugins}" "$checkout/src"
    fi
    "$checkout/src/scripts/install.sh"
    exit 0
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ -n "${PIG_PLUGINS_COMMIT:-}" ]; then
    actual_commit=$(git -C "$root" rev-parse HEAD)
    [ "$actual_commit" = "$PIG_PLUGINS_COMMIT" ] || { echo "install: checkout does not match pinned commit" >&2; exit 1; }
fi
pig_home=${PIG_HOME:-"$HOME/.pig"}
agent_dir=${PIG_CODING_AGENT_DIR:-"$pig_home/agent"}
dest="$pig_home/pig-plugins"

for tool in git tar; do
    command -v "$tool" >/dev/null 2>&1 || { echo "install: $tool is required on PATH" >&2; exit 1; }
done
[ -d "$root/.git" ] || [ -f "$root/.git" ] || { echo "install: run from a git checkout of pig-plugins" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "install: Go is required on PATH" >&2; exit 1; }
compatible_version=$(cd "$root" && GOWORK=off go run ./cmd/compatibility "$root/COMPATIBILITY.json")
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    [ -f "$root/prompts/agent/$f" ] || { echo "install: missing prompts/agent/$f" >&2; exit 1; }
done

mkdir -p -- "$pig_home" "$agent_dir" "$pig_home/bin"

log=$(mktemp "${TMPDIR:-/tmp}/pig-plugins-install-log.XXXXXX")
trap 'rm -f "$log"' 0
# fail_with_log MESSAGE: print the error and the captured output of the failed step.
fail_with_log() {
    echo "install: $1" >&2
    cat -- "$log" >&2
    exit 1
}

# 1. Ensure PiG is installed at the release declared in COMPATIBILITY.json.
echo "install: [1/7] checking PiG installation" >&2
if [ "${PIG_PLUGINS_UPDATE:-0}" = 1 ] || ! command -v pig >/dev/null 2>&1; then
    command -v curl >/dev/null 2>&1 || { echo "install: curl is required to install pig" >&2; exit 1; }
    upstream_installer=$(mktemp "${TMPDIR:-/tmp}/pig-install.XXXXXX")
    if ! curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 10 --max-time 60 https://pi-in-go.dev/install.sh -o "$upstream_installer" >"$log" 2>&1; then
        rm -f "$upstream_installer"
        fail_with_log "PiG installer download failed"
    fi
    if ! PIG_VERSION="${PIG_VERSION:-$compatible_version}" sh "$upstream_installer" >"$log" 2>&1; then
        rm -f "$upstream_installer"
        fail_with_log "pig installation failed"
    fi
    rm -f "$upstream_installer"
    # The installer writes to ~/.local/bin by default, which may not be on PATH yet.
    PATH="${PIG_INSTALL_DIR:-$HOME/.local/bin}:$PATH"
    export PATH
    command -v pig >/dev/null 2>&1 || { echo "install: pig installation failed" >&2; exit 1; }
fi

# 2. Reuse any ast-grep version, or install a verified official native release.
echo "install: [2/7] checking ast-grep installation" >&2
PATH="$PATH:$pig_home/bin"
export PATH
if command -v ast-grep >/dev/null 2>&1; then
    echo "install: using existing ast-grep on PATH" >&2
else
    (cd "$root" && GOWORK=off go run ./cmd/install-ast-grep "$pig_home/bin") || {
        echo "install: ast-grep installation failed; install it on PATH and retry" >&2
        exit 1
    }
fi

# 3. Build the fused executable. Prompts and skills follow a successful build.
echo "install: [3/7] building executable" >&2
binary=$("$root/scripts/dev_build.sh" "$pig_home/bin/pig-plugins") || { echo "install: build failed (see diagnostics above)" >&2; exit 1; }

# 4. Copy tracked files to a staging directory, then swap it in.
echo "install: [4/7] copying repository files" >&2
stage=$(mktemp -d "$pig_home/.pig-plugins-stage.XXXXXX")
trap 'rm -rf "$stage"; rm -f "$log"' 0
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

# 5. Copy the prompts into the agent directory.
echo "install: [5/7] installing agent prompts" >&2
stamp=$(date +%s)
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    target="$agent_dir/$f"
    if [ -e "$target" ] && ! cmp -s "$dest/prompts/agent/$f" "$target"; then
        cp -- "$target" "$target.pig-plugins-backup-$stamp"
    fi
    cp -- "$dest/prompts/agent/$f" "$target.tmp.$$"
    mv -- "$target.tmp.$$" "$target"
done

# Default keybindings (ctrl+z undoes instead of suspending). keybindings.json is user-owned, so an
# existing different file is kept untouched rather than overwritten.
keys_source="$dest/config/agent/keybindings.json"
keys_target="$agent_dir/keybindings.json"
if [ -f "$keys_source" ]; then
    if [ ! -e "$keys_target" ]; then
        cp -- "$keys_source" "$keys_target.tmp.$$"
        mv -- "$keys_target.tmp.$$" "$keys_target"
    elif ! cmp -s "$keys_source" "$keys_target"; then
        echo "install: $keys_target exists and differs; not replacing it (defaults: $keys_source)" >&2
    fi
fi

# 6. Copy the skills into the agent directory.
echo "install: [6/7] installing skills" >&2
for skill in "$dest"/skills/*/; do
    [ -f "$skill/SKILL.md" ] || continue
    name=$(basename -- "$skill")
    target="$agent_dir/skills/$name"
    mkdir -p -- "$agent_dir/skills"
    if [ -e "$target" ] && ! diff -rq -- "$skill" "$target" >/dev/null 2>&1; then
        # Backups stay outside skills/ so PiG does not discover them as duplicate skills.
        mkdir -p -- "$agent_dir/skills-backup/$stamp"
        mv -- "$target" "$agent_dir/skills-backup/$stamp/$name"
    fi
    if [ ! -e "$target" ]; then
        cp -R -- "$skill" "$target.tmp.$$"
        mv -- "$target.tmp.$$" "$target"
    fi
done

# Retire the previous launcher payloads only after successful installation.
rm -f -- "$pig_home/bin/pig-plugins-native" "$pig_home/bin/pig-plugins-raw"

# 7. Link the executable into the user bin directory so `pig-plugins` is on PATH.
echo "install: [7/7] linking executable" >&2
link_dir=${PIG_PLUGINS_LINK_DIR:-"$HOME/.local/bin"}
link="$link_dir/pig-plugins"
mkdir -p -- "$link_dir"
if [ -e "$link" ] && [ ! -L "$link" ]; then
    echo "install: '$link' exists and is not a symlink; not replacing it" >&2
else
    ln -sfn -- "$binary" "$link"
    case ":$PATH:" in
        *":$link_dir:"*) ;;
        *) echo "install: $link_dir is not on PATH; add it to run 'pig-plugins' directly" >&2 ;;
    esac
fi

echo "pig-plugins install completed, you may use it with \`pig-plugins\`"
