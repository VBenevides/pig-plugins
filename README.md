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

## hashline-edit

Replaces the built-in `read` and `edit` with anchored read (`<line>#<hash>|text`, four hex digits of SHA-1) and strict
anchored edit. It ports the `harness-hashline` twin from zed-pi-harness, not the upstream npm `pi-hashline-edit`
0.8.3, which uses a different anchor format (`LINE#HASH:` with two-character hashes) and extra ops. The outline for
large source files uses the same regular expressions as the original; `internal/hashline/testdata/ts-golden.json` holds
the TypeScript outputs it is compared against (`.agent-work/scripts/hashline-golden/generate.mjs` regenerates it).
Differences: the SDK has no handle on the replaced stock read, so images (attached unresized, up to 5 MiB), non-UTF-8
text, directories and missing files are handled by `internal/hashline` itself; edits of one file are serialised by an
in-process lock instead of Pi's file mutation queue.

## LANCET model runtime

`internal/lancet` is a native Go port of the LANCET v0.4.3 classifier (byte-level BPE tokenizer, int8 encoder, pooled
linear head, Platt calibration). The encoder runs in `libonnxruntime` loaded with `dlopen` through
`github.com/shota3506/onnxruntime-purego`: PiG builds extensions with cgo disabled, so a cgo binding such as
`yalue/onnxruntime_go` cannot compile there. The model files and the shared library stay outside the repository;
`Load(dir, libraryPath)` verifies every model file against the pinned size and SHA-256 before use. Tests skip when
they are absent; set `LANCET_MODEL_DIR` and `LANCET_ORT_LIBRARY` to point at them.

## smart-approve-lancet

The safety layer: it registers no tool and gates `bash`, `write` and `edit` through `tool_call`. Hard-blocked bash
behaviors (`rm -rf /`, `curl | sh`, fork bombs, ...) are always blocked. Other dangerous behaviors and protected paths
(`~/.ssh`, `.env`, ... with `.env.example` allowed; symlinks are resolved, dangling ones included) ask for
confirmation in `interactive` mode and are blocked in `strict` mode or when no UI exists. With `/smart-approve-lancet
lancet on`, every other bash command is scored locally first: `risky` is blocked, `review` asks, `not_flagged`
continues, and an unavailable model blocks. A damaged settings file (`smart-approve-lancet.json` in the agent
directory) means `strict` and is reported. Commands: `/smart-approve-lancet [interactive|strict|status]` (no argument
toggles) and `/smart-approve-lancet lancet [status|setup|on|off|check <command>]`.

The pattern tables are a port of the `behaviors.ts` and `paths.ts` of zed-pi-harness `smart-approve`;
`internal/guard/testdata/ts-golden.json` holds the TypeScript verdicts for 402 commands, 93 delete targets, 27
normalizer inputs and 81 paths (`.agent-work/scripts/smart-approve-golden/generate.mjs` regenerates it). JavaScript
regexps run in `regexp2` with its ECMAScript mode, a 2 s match timeout, and `\s` widened to the JavaScript whitespace set.

Differences from the TypeScript original:

- Block reasons start with `smart-approve-lancet: ` instead of `harness-guard: `.
- No LLM risk analysis and no `auto` mode, as in the TypeScript twin `harness-guard`.
- `.` in a path glob also matches a line break, so a newline in a path cannot dodge a `**` pattern.
- The ONNX Runtime library is not bundled. `lancet setup` downloads the pinned model (LANCET Nano v0.4.3) and the pinned
  ONNX Runtime 1.30.0 archive for linux/amd64, linux/arm64 or darwin/arm64 into `<agent dir>/smart-approve-lancet/`;
  both are checked for size and SHA-256 before use, and the library is hashed again before every load. Set
  `LANCET_ORT_LIBRARY` to use another library (and on other platforms).
- A native scoring call cannot be interrupted once it started; scoring is bounded by a 60 s timeout between steps.

Do not enable `harness-guard`, the TypeScript twin, at the same time.
