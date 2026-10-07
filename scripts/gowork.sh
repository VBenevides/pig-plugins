#!/bin/sh
# Create the ignored go.work that points this module at PiG's staged Go SDK, so plain `go` commands
# resolve github.com/MichaelKinsy/PiG/extensions/sdk. `pig` does the same replacement itself at build time.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
sdk=$(pig reload --sdk-path)
if [ ! -f "$sdk/go.mod" ]; then
    echo "gowork: PiG SDK not found at '$sdk' (run pig once, or 'pig extension init' to stage it)" >&2
    exit 1
fi
cd "$root"
rm -f go.work go.work.sum
go work init .
# A workspace `use` is not enough once a third-party module is imported: Go then loads the module graph and
# tries to fetch the placeholder v0.0.0 of the SDK. A workspace replace answers that lookup locally.
go work edit -replace "github.com/MichaelKinsy/PiG/extensions/sdk@v0.0.0=$sdk"
echo "gowork: wrote $root/go.work (sdk: $sdk)"
