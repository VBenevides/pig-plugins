#!/bin/sh
# Create the ignored workspace using the patched SDK required by native extensions.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
version=$(cd "$root" && GOWORK=off go run ./cmd/compatibility "$root/COMPATIBILITY.json")
ref="github.com/MichaelKinsy/PiG/extensions/sdk@v$version"
GOWORK=off go mod download "$ref"
source=$(GOWORK=off go list -m -f '{{.Dir}}' "$ref")
mkdir -p "$root/build"
stage=$(mktemp -d "$root/build/native-sdk.XXXXXX")
trap 'rm -rf "$stage"' 0
cp -R "$source/." "$stage"
chmod -R u+w "$stage"
GIT_CEILING_DIRECTORIES="$root" git -C "$stage" apply "$root/patches/pig/0002-native-oauth-accounts-sdk.patch"
GIT_CEILING_DIRECTORIES="$root" git -C "$stage" apply "$root/patches/pig/0011-conditional-draft-editor-sdk.patch"
GIT_CEILING_DIRECTORIES="$root" git -C "$stage" apply "$root/patches/pig/0013-native-image-preview-sdk.patch"
sdk="$root/build/native-sdk"
rm -rf "$sdk"
mv "$stage" "$sdk"
cd "$root"
rm -f go.work go.work.sum
go work init .
go work edit -replace "github.com/MichaelKinsy/PiG/extensions/sdk=$sdk"
echo "gowork: wrote $root/go.work (sdk: $sdk)"
