# pig-plugins

Native Go extensions for [PiG](https://github.com/MichaelKinsy/PiG) (the Go port of Pi). Each folder under
`extensions/<name>/` is one conventional factory, `func Extension() *sdk.Extension`, whose registered identity
equals the folder name. The repository is one Go module so extensions share `internal/` packages.

## Layout

| Path | Purpose |
|---|---|
| `extensions/<name>/` | One extension (a Go package). |
| `internal/agentdir` | Agent directory resolution (`PIG_CODING_AGENT_DIR`, `PIG_HOME/agent`, `~/.pig/agent`). |
| `internal/fsutil` | Atomic file writes and JSON settings files. |
| `internal/pigtest` | Runs the real `pig` against a scripted OpenAI-compatible mock model in an isolated HOME. |
| `testfixtures/echo` | Test-only extension that proves the harness end to end. It lives outside `internal/` because pig's generated runner imports the extension package. |

## Develop

```sh
make gowork     # once: creates the ignored go.work that points at PiG's staged SDK
make check      # gofmt, go vet, go test, and `pig install --validate-only` for every extension
pig -e ./extensions/<name>          # load one extension into a session
```

`go.mod` carries `require github.com/MichaelKinsy/PiG/extensions/sdk v0.0.0` and no machine path. `pig` replaces
it with the version-matched staged SDK at build time; `go.work` does the same for plain `go` commands.

Tests that start `pig` skip when it is not on `PATH`.

## Parity

Each extension ports an existing TypeScript original and must behave the same. Parity tests compare the Go output
with golden files captured from the original (`testdata/` next to the test). `pigtest.Golden` has no update flag on
purpose: regenerating a golden from the Go port would erase the check.

Do not enable a Go port together with its TypeScript twin: PiG rejects duplicate tool names.

## LANCET model runtime

`internal/lancet` is a native Go port of the LANCET v0.4.3 classifier (byte-level BPE tokenizer, int8 encoder, pooled
linear head, Platt calibration). The encoder runs in `libonnxruntime` loaded with `dlopen` through
`github.com/shota3506/onnxruntime-purego`: PiG builds extensions with cgo disabled, so a cgo binding such as
`yalue/onnxruntime_go` cannot compile there. The model files and the shared library stay outside the repository;
`Load(dir, libraryPath)` verifies every model file against the pinned size and SHA-256 before use. Tests skip when
they are absent; set `LANCET_MODEL_DIR` and `LANCET_ORT_LIBRARY` to point at them.
