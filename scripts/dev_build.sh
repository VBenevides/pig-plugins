#!/bin/sh
# Build a target-native PiG executable with this repository's selected extensions.
# Build diagnostics go to stderr; stdout contains only the executable's absolute path.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "$#" -gt 1 ]; then
    echo "usage: $0 [output-path]" >&2
    exit 2
fi
if ! command -v go >/dev/null 2>&1; then
    echo "dev_build: Go is required on PATH" >&2
    exit 1
fi
if ! command -v git >/dev/null 2>&1; then
    echo "dev_build: git is required on PATH" >&2
    exit 1
fi
if ! command -v tar >/dev/null 2>&1; then
    echo "dev_build: tar is required on PATH" >&2
    exit 1
fi
# Bound compiler concurrency on desktops; callers can explicitly override it.
GOMAXPROCS=${GOMAXPROCS:-2}
GOFLAGS="${GOFLAGS:-} -p=${PIG_BUILD_JOBS:-2}"
export GOMAXPROCS GOFLAGS
echo "dev_build: [1/6] resolving PiG source (Go jobs: ${PIG_BUILD_JOBS:-2})" >&2

out=${1:-"$root/build/pig-plugins"}
case "$out" in
    /*) ;;
    *) out="$PWD/$out" ;;
esac
if [ -d "$out" ]; then
    echo "dev_build: output path is a directory: '$out'" >&2
    exit 1
fi
mkdir -p -- "$(dirname -- "$out")"

# Resolve local resources from the repository, even when invoked from elsewhere.
cd "$root"

compatible_version=$(GOWORK=off go run ./cmd/compatibility "$root/COMPATIBILITY.json")
# Explicit candidate override keeps supported compatibility metadata unchanged.
compatible_version=${PIG_BUILD_VERSION:-$compatible_version}

# PiG's fused builder adds local module replacements, but not their sums.
# A workspace makes the extension module's verified dependency graph available.
source=${PIG_SOURCE_ROOT:-}
if [ -z "$source" ]; then
    ref="github.com/MichaelKinsy/PiG@v$compatible_version"
    GOWORK=off go mod download "$ref" >&2
    source=$(GOWORK=off go list -m -f '{{.Dir}}' "$ref")
fi
if [ ! -f "$source/go.mod" ] || [ ! -d "$source/cmd/pig" ]; then
    echo "dev_build: '$source' is not a PiG source checkout" >&2
    exit 1
fi
mkdir -p "$root/build"
stage=$(mktemp -d "$root/build/dev-source.XXXXXX")
artifact_dir=
trap 'rm -rf "$stage"; if [ -n "$artifact_dir" ]; then rm -rf "$artifact_dir"; fi' 0
trap 'exit 130' INT
trap 'exit 143' TERM
echo "dev_build: [2/6] staging source (excluding build/cache/worktree data)" >&2
mkdir "$stage/source"
# Never copy checkout-local artifacts: they can include this staging directory
# itself, nested worktrees, module caches, or gigabytes of previous builds.
tar -C "$source" --exclude='./.git' --exclude='./.agent-work' \
    --exclude='./build' --exclude='./node_modules' --exclude='./.ouro' \
    -cf "$stage/source.tar" .
tar -C "$stage/source" -xf "$stage/source.tar"
rm -f "$stage/source.tar"
chmod -R u+w "$stage/source"
# Go extension factories resolve to this repository's module root. Keep local
# workspaces, scratch environments, and tool caches out of its build-lock walk.
mkdir "$stage/plugins"
tar -C "$root" --exclude='./.git' --exclude='./.agent-work' \
    --exclude='./build' --exclude='./node_modules' --exclude='./.ouro' \
    --exclude='./.ruff_cache' --exclude='__pycache__' \
    --exclude='./go.work' --exclude='./go.work.sum' \
    -cf "$stage/plugins.tar" .
tar -C "$stage/plugins" -xf "$stage/plugins.tar"
rm -f "$stage/plugins.tar"
echo "dev_build: [3/6] applying native host patches" >&2
# The native account extension requires the pinned host and SDK patches.
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0001-accounts-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0003-editor-images-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0005-provider-recovery-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0007-distribution-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0008-settings-diagnostics-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0010-tmux-images-host.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/source" apply "$root/patches/pig/0011-editor-navigation-host.patch" >&2
echo "dev_build: [4/6] preparing patched SDK" >&2
sdk_module=github.com/MichaelKinsy/PiG/extensions/sdk
sdk_ref="$sdk_module@v$compatible_version"
GOWORK=off go mod download "$sdk_ref" >&2
sdk_source=$(GOWORK=off go list -m -f '{{.Dir}}' "$sdk_ref")
mkdir "$stage/sdk"
cp -R "$sdk_source/." "$stage/sdk"
chmod -R u+w "$stage/sdk"
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/sdk" apply "$root/patches/pig/0002-accounts-sdk.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/sdk" apply "$root/patches/pig/0004-editor-images-sdk.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/sdk" apply "$root/patches/pig/0006-provider-recovery-sdk.patch" >&2
GIT_CEILING_DIRECTORIES="$root" git -C "$stage/sdk" apply "$root/patches/pig/0009-compaction-sdk.patch" >&2
(
    cd "$stage/source"
    rm -f go.work go.work.sum
    GOWORK=off go work init . "$stage/plugins"
    GOWORK="$stage/source/go.work" go work edit -replace "github.com/MichaelKinsy/PiG/extensions/sdk=$stage/sdk"
    # Planning runs in the builder process, not the target source tree.
    # Use the patched host for both planning and the bundled executable.
    echo "dev_build: [5/6] compiling build host (packages shown as compiled)" >&2
    GOWORK="$stage/source/go.work" go build -v -buildvcs=false -o "$stage/pig-builder" ./cmd/pig >&2
)

# PiG refuses existing outputs. Build beside the destination, then replace it
# only after verification; a failed rebuild leaves the previous binary usable.
artifact_dir=$(mktemp -d "$(dirname -- "$out")/.pig-dev-build.XXXXXX")
echo "dev_build: [6/6] building fused executable" >&2
# Never bake the builder user's models, MCP servers, or discovery settings.
PIG_HOME="$stage/build-home" PIG_CODING_AGENT_DIR="$stage/build-home/agent" \
PI_CODING_AGENT_DIR="$stage/build-home/pi-agent" \
PIG_SOURCE_ROOT="$stage/source" "$stage/pig-builder" piglet build "$stage/plugins/piglet.yaml" --format binary --out "$artifact_dir/pig-plugins" >&2
if [ ! -x "$artifact_dir/pig-plugins" ]; then
    echo "dev_build: builder did not produce an executable" >&2
    exit 1
fi
mv -f -- "$artifact_dir/pig-plugins" "$out"
echo "dev_build: completed: $out" >&2
printf '%s\n' "$out"
