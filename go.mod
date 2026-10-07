module github.com/VBenevides/pig-plugins

go 1.26

// Pig resolves this requirement to the version-matched staged SDK at build time.
// For local `go build`/`go vet`/`go test`, run scripts/gowork.sh (creates the ignored go.work).
require github.com/MichaelKinsy/PiG/extensions/sdk v0.0.0
