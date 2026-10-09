module github.com/VBenevides/pig-plugins

go 1.26

// Pig resolves this requirement to the version-matched staged SDK at build time.
// For local `go build`/`go vet`/`go test`, run scripts/gowork.sh (creates the ignored go.work).
require github.com/MichaelKinsy/PiG/extensions/sdk v0.0.0

require (
	github.com/dlclark/regexp2 v1.12.0
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/shota3506/onnxruntime-purego v0.0.0-20260315223538-8db8bd7424b2
	golang.org/x/image v0.45.0
)
