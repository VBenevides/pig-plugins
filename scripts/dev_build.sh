#!/bin/sh
# Build a target-native PiG executable with this repository's selected extensions.
# Build diagnostics go to stderr; stdout contains only the executable's absolute path.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "$#" -gt 1 ]; then
    echo "usage: $0 [output-path]" >&2
    exit 2
fi
if ! command -v pig >/dev/null 2>&1; then
    echo "dev_build: pig is required on PATH" >&2
    exit 1
fi
if ! command -v go >/dev/null 2>&1; then
    echo "dev_build: Go is required on PATH" >&2
    exit 1
fi

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

# PiG 0.4.1's fused builder adds local module replacements, but not their sums.
# A workspace makes the extension module's verified dependency graph available.
source=${PIG_SOURCE_ROOT:-}
if [ -z "$source" ]; then
    version=$(pig --version)
    version=${version#pig }
    version=${version%%+*}
    ref="github.com/MichaelKinsy/PiG@v$version"
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
mkdir "$stage/source"
cp -R "$source/." "$stage/source"
chmod -R u+w "$stage/source"
(
    cd "$stage/source"
    rm -f go.work go.work.sum
    GOWORK=off go work init . "$root"
    sdk_module=github.com/MichaelKinsy/PiG/extensions/sdk
    if [ -f extensions/sdk/go.mod ]; then
        GOWORK="$stage/source/go.work" go work edit -replace "$sdk_module=$stage/source/extensions/sdk"
    else
        sdk_version=$(GOWORK=off go list -m -f '{{.Version}}' "$sdk_module")
        GOWORK="$stage/source/go.work" go work edit -replace "$sdk_module@v0.0.0=$sdk_module@$sdk_version"
    fi
)

# PiG refuses existing outputs. Build beside the destination, then replace it
# only after verification; a failed rebuild leaves the previous binary usable.
artifact_dir=$(mktemp -d "$(dirname -- "$out")/.pig-dev-build.XXXXXX")
PIG_SOURCE_ROOT="$stage/source" pig piglet build "$root/piglet.yaml" --format binary --out "$artifact_dir/pig-plugins" >&2
if [ ! -x "$artifact_dir/pig-plugins" ]; then
    echo "dev_build: builder did not produce an executable" >&2
    exit 1
fi
mv -f -- "$artifact_dir/pig-plugins" "$out"
printf '%s\n' "$out"
