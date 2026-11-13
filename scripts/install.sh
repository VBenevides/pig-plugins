#!/bin/sh
# Install pig-plugins by copying (never linking) it into PiG's home.
# Installs the compatible PiG release (override with PIG_VERSION) when `pig` is not on PATH, then builds the
# fused executable; prompts and skills are installed only after that build succeeds.
#
#   <home>/pig-plugins/            tracked repository files, including every extension and prompts/
#   <home>/bin/pig-plugins         fused PiG executable with distribution update support
#   <agent>/{SYSTEM,APPEND_SYSTEM,AGENTS}.md   not written: the binary embeds the prompts. A file here overrides the
#                                              built-in prompt; a copy that an earlier release installed is removed
#   <agent>/keybindings.json                   copied from config/agent/ only when absent; never overwritten
#   <agent>/skills/<name>/         copied from skills/ (every directory holding a SKILL.md); a differing
#                                  existing skill moves to <agent>/skills-backup/<timestamp>/<name>
#   <agent>/skills/ouro-quality/   the output of `ouro skill`. The installer installs or updates ouro from
#                                  https://github.com/VBenevides/Ouro when it is missing or its version differs from
#                                  main's VERSION. The new text replaces the old skill without a backup. A failure only warns, and the
#                                  previous skill stays in place
#
# <home> is $PIG_HOME or ~/.pig; <agent> is $PIG_CODING_AGENT_DIR or <home>/agent.
# Re-running replaces the copy with the current repository state.
# Installation stages and build progress are printed to stderr.
# PiG bootstrap output is retained in a temporary log and shown on failure.
set -eu

install_cursor=0
for argument in "$@"; do
    case "$argument" in
        --install-cursor) install_cursor=1 ;;
        *) echo "install: unknown option: $argument" >&2; exit 2 ;;
    esac
done

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
    "$checkout/src/scripts/install.sh" "$@"
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
for f in default/SYSTEM.md; do
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

# 5. The binary embeds the prompts. Remove only copies that earlier releases installed (known checksums), so the
# built-in prompt applies again. Any other file is the user's override and stays.
echo "install: [5/7] retiring installed agent prompts" >&2
stamp=$(date +%s)
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
for f in SYSTEM.md APPEND_SYSTEM.md AGENTS.md; do
    target="$agent_dir/$f"
    [ -f "$target" ] || continue
    case "$(sha256_of "$target")" in
        c5348b6307935cce1f72b730d9db4d10e02157225fc2b3bc9f5cf472dd24c8ce|c66a4d38b37d40182ecc8dc27bab72f7007f2336881adbd160caade744a8c502|568bfc7cf402967bb6a7929a06b3e25a8014b9403fd09203be85721555dafe39|ebf45e8909fa597a681479700c09325d4a1569c458f3511df4d2fb217040210f|74e5731d6c87de290cf5e13cafdec0ed24491d35d9b9e159c3b5881c6f64ee28|2ef62a6146185a28a3acb2e3ba55f0d50fe4b60636813af4c2909c3e5e851d70)
            rm -- "$target"
            echo "install: removed installed $f; the built-in prompt applies" >&2
            ;;
        *) echo "install: keeping $target; it overrides the built-in prompt" >&2 ;;
    esac
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

# install_skill SOURCE NAME: copy one skill directory into the agent directory. A differing existing skill moves
# to skills-backup first.
install_skill() {
    target="$agent_dir/skills/$2"
    mkdir -p -- "$agent_dir/skills"
    if [ -e "$target" ] && ! diff -rq -- "$1" "$target" >/dev/null 2>&1; then
        # Backups stay outside skills/ so PiG does not discover them as duplicate skills.
        mkdir -p -- "$agent_dir/skills-backup/$stamp"
        mv -- "$target" "$agent_dir/skills-backup/$stamp/$2"
    fi
    if [ ! -e "$target" ]; then
        cp -R -- "$1" "$target.tmp.$$"
        mv -- "$target.tmp.$$" "$target"
    fi
}

# sync_ouro WORKDIR: make the installed ouro match the VERSION on Ouro's main branch, then save the text of
# `ouro skill` as WORKDIR/ouro-quality/SKILL.md. Ouro owns that skill; this repository does not ship it.
# A failure returns non-zero after printing the cause, so an offline install still finishes.
sync_ouro() {
    ouro_raw=https://raw.githubusercontent.com/VBenevides/Ouro/main
    command -v curl >/dev/null 2>&1 || { echo "install: curl is required to install ouro" >&2; return 1; }
    curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 10 --max-time 60 --max-filesize 4096 \
        "$ouro_raw/VERSION" -o "$1/VERSION" >"$log" 2>&1 || { echo "install: could not read the Ouro version" >&2; cat -- "$log" >&2; return 1; }
    ouro_remote=$(tr -d '[:space:]' <"$1/VERSION")
    case "$ouro_remote" in
        ''|*[!0-9A-Za-z.+-]*) echo "install: invalid Ouro version from main" >&2; return 1 ;;
    esac
    ouro_local=
    if command -v ouro >/dev/null 2>&1; then
        ouro_local=$(ouro version 2>/dev/null | tr -d '[:space:]') || ouro_local=
    fi
    if [ "$ouro_local" = "$ouro_remote" ]; then
        echo "install: ouro $ouro_local is current" >&2
    else
        echo "install: installing ouro $ouro_remote (installed: ${ouro_local:-none})" >&2
        curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 10 --max-time 60 --max-filesize 1048576 \
            "$ouro_raw/install.sh" -o "$1/install.sh" >"$log" 2>&1 || { echo "install: could not download the Ouro installer" >&2; cat -- "$log" >&2; return 1; }
        sh "$1/install.sh" >"$log" 2>&1 || { echo "install: ouro installation failed (it needs git and go)" >&2; cat -- "$log" >&2; return 1; }
        # Ouro installs to ~/.local/bin by default, which may not be on PATH yet.
        PATH="${OURO_INSTALL_DIR:-$HOME/.local/bin}:$PATH"
        export PATH
        command -v ouro >/dev/null 2>&1 || { echo "install: ouro is not on PATH after installation" >&2; return 1; }
    fi
    mkdir -p -- "$1/ouro-quality"
    ouro skill >"$1/ouro-quality/SKILL.md" 2>"$log" || { echo "install: 'ouro skill' failed" >&2; cat -- "$log" >&2; return 1; }
    grep -q '^name: ouro-quality$' "$1/ouro-quality/SKILL.md" || { echo "install: 'ouro skill' did not print the ouro-quality skill" >&2; return 1; }
}

# 6. Copy the skills into the agent directory.
echo "install: [6/7] installing skills" >&2
for skill in "$dest"/skills/*/; do
    [ -f "$skill/SKILL.md" ] || continue
    install_skill "$skill" "$(basename -- "$skill")"
done
ouro_work=$(mktemp -d "${TMPDIR:-/tmp}/pig-ouro.XXXXXX")
if sync_ouro "$ouro_work"; then
    # Ouro owns this skill, so the new text replaces the old one without a backup.
    mkdir -p -- "$agent_dir/skills"
    rm -rf -- "$agent_dir/skills/ouro-quality"
    cp -R -- "$ouro_work/ouro-quality" "$agent_dir/skills/ouro-quality"
else
    echo "install: ouro-quality skill not updated; fix the error above and run the installer again" >&2
fi
rm -rf -- "$ouro_work"


if [ "$install_cursor" -eq 1 ]; then
    echo "install: installing optional Cursor SDK Bridge" >&2
    PIG_CODING_AGENT_DIR="$agent_dir" "$binary" --install-cursor
fi
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
