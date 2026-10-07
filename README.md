# pig-plugins

Native Go extensions for [PiG](https://github.com/MichaelKinsy/PiG) (the Go port of Pi). Each folder under
`extensions/<name>/` is one conventional factory, `func Extension() *sdk.Extension`, whose registered identity
equals the folder name. The repository is one Go module so extensions share `internal/` packages.

## Port status

Implemented: `hashline-edit`, `smart-approve-lancet`, `pi-curator`, `lsp`,
`web-search`, `ask-user-question`, and `todo`.
Use `pig -e ./extensions/<name>` to load a port for one session.
Validation with `make check` does not enable these extensions in the default configuration.

`better-footer` and `auto-models` remain blocked, not partially implemented.
PiG's native extension context does not expose whether startup model/provider/thinking flags were explicit.
The verified `GetFlag` probe returned null both with implicit defaults and explicit CLI selections.
Upstream footer restoration must respect that precedence; upstream auto-models must disable switching after `--model`.
A startup-options API or an explicitly approved contract change is required.
Workspace-plus-conversation `rewind` remains deferred by user choice.

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

Each extension ports an existing TypeScript original. Parity tests compare Go output with golden files captured
from that original (`testdata/` or `testfixtures/`). Approved host limits and intentional safety differences are
listed in each extension's section. `pigtest.Golden` has no update flag: Go output must not regenerate its own baseline.

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

## pi-curator

The native Go extension captures visible conversation text and tool traffic in consented repositories.
It calls `PI_CURATOR_BIN`, or `curator` on PATH. The curator packages are internal to another Go module.
The CLI owns redaction, locking, the journal format, search, and UTF-8 cursor pages.
Existing `.curator` journals stay readable. The extension never writes the journal directly.

For a repository without memory, the extension asks for consent before the first prompt.
PiG RPC cannot answer a dialog during `session_start`. The deferred question avoids that startup deadlock.
Headless sessions require prior operator consent through `curator init --consent --cwd <repository>`.
Thinking blocks and `read` results are not captured.
One background worker drains a bounded queue. Overflow and undelivered events become capture gaps.
Shutdown cancels that worker before two bounded drain attempts. Any unjournaled gaps remain visible in warnings.

`memory_search` and `memory_read` are deferred tools.
If `tool_search` or `codemode` is active, the model discovers the tools on demand.
Otherwise, a consented session activates them directly.
The tools reject access outside the session's repository.
Read budgets cover the complete serialized response, including metadata, escaping, and the final newline.
Use `next_cursor` with one event ID to read the next page.

`/pi-curator [status|prefetch <off|64..8192>|startup <off|1..10>|engine <legacy|fts|hybrid|episodes|state>]`
stores settings in `<agent dir>/pi-curator.json` with mode 0600.
Saved settings take precedence over environment variables.
The integration defaults are prefetch 512, startup 2, and engine legacy.
Set `PI_CURATOR_PREFETCH_BUDGET=0` to disable task prefetch.
Set `PI_CURATOR_STARTUP_DECISIONS=0` to disable startup history as well.
`PI_CURATOR_SEARCH_ENGINE` selects the tool engine. `PI_CURATOR_PREFETCH_ENGINE` separately selects the prefetch engine.
History enters an ordinary hidden message, never the system prompt.
The system prompt receives only static recall guidance. The extension adds no memory-access widget.

Parity fixtures come from the original TypeScript event mapper, settings parser, and prefetch expression.
Go maps lose argument insertion order. Tool arguments and paths therefore use deterministic key ordering.
Unlike the adapter, default settings are described as defaults rather than environment values.
The Go queue also bounds bytes, so an oversized event cannot prevent its valid neighbors from reaching curator.
Repository roots are checked again before each subprocess.
The CLI has no atomic expected-repository parameter. Concurrent replacement of repository metadata remains a limitation.

Do not enable `harness-curator`, the TypeScript twin, at the same time.

## lsp

The native Go extension follows `pi-lsp@0.1.7` for the core tool contract and JSON configuration.
Positions use zero-based `line` and UTF-16 `character`.
It adds read-only `lsp_rename_preview`, `code_overview`, and `code_search` from the `harness-code` reference.
Structural search uses the existing outline heuristics, not an AST parser.
Query and body expressions use bounded ECMAScript regexes.
All eight tools are deferred. A session without `tool_search` or `codemode` activates them directly.

Global configuration lives at `<agent dir>/lsp.json`.
`PI_AGENT_DIR`, when set, selects the original upstream configuration directory.
The nearest `.pi/lsp.json` overrides global servers by ID.
Project configuration requires hash-based approval before its commands can run.
The choices are `Trust once`, `Trust always`, and `Reject`.
Persistent hashes live at `<agent dir>/trust/lsp.json`. A changed file requires new approval.
Headless sessions reject an untrusted project configuration.
Commands use executable-plus-arguments spawning, never a shell.
The original `{workspace}`, `{root}`, `{file}`, `{relFile}`, `{dir}`, `{relDir}`, `{config}`, and `{configDir}` templates remain available.

When no global configuration exists, the extension uses the `harness-code` language-server registry.
It searches operator PATH, `ZED_PI_HARNESS_LSP_SEARCH_PATH`, and the Neovim Mason bin directory.
It never installs a server. It does not search repository `node_modules/.bin` implicitly.
Declare a local executable explicitly in a trusted project configuration instead.
`ZED_PI_HARNESS_LSP_<FAMILY>` accepts a JSON argv array or whitespace-separated command.
Families are typescript, python, go, rust, bash, lua, markdown, json, and c.

The extension starts servers lazily and synchronizes disk text before requests.
Successful `write` and `edit` calls append diagnostics from each matching server.
Servers that request `didSave` receive that notification.
Diagnostics requests refresh current disk state instead of treating an empty cache as a clean result.
A server that sends no diagnostics produces an explicit incomplete-analysis result.
The post-edit hook has a six-second deadline.
Requests, initialization, framing, document size, open-document count, and server-pool size have limits.
Shutdown joins the server and terminates its process group, including surviving helpers.
The client refuses every `workspace/applyEdit` request. Rename previews never change files.

Real gopls output was compared with the TypeScript reference on one isolated Go fixture.
Hover, definition, references, document symbols, workspace symbols, rename edits, and diagnostics matched.
Mock-LLM PiG runs cover all eight tools, post-edit diagnostics, and rejection of untrusted project commands.
Do not enable `harness-code`, the TypeScript twin, at the same time.

## web-search

The native Go extension ports `pi-web-search@1.6.0`.
`web_search` uses provider-native tools, not a general HTTP search service.
`web_search` is deferred. Enable `builtin:tool-search` or `codemode` to discover it.
Sessions without either discovery tool activate it directly.
With an explicit `--tools` allowlist, include `web_search` in that allowlist; its deferred exposure still keeps it out of the initial model declaration.
Supported transports are Google Gemini, OpenAI Responses (including Azure, Codex and Copilot),
xAI Responses, and Anthropic Messages. OpenCode Zen/Go receive their session attribution headers.
`url_context` uses Gemini URL Context and sends YouTube URLs as video parts.
It is active only for a compatible conversation model. Tool activation remembers manual enable/disable choices.
Both tools accept `query`; `web_search` accepts an optional `urls` array, while `url_context` requires 1–20 URLs.

Search uses the current conversation model unless `<agent dir>/web-search.json` explicitly selects one:

```json
{"provider": "openai", "model": "gpt-5.5"}
```

`modelId` is accepted as the upstream alternative. `PI_WEB_SEARCH_CONFIG` selects another configuration file.
Invalid configuration and unsupported models return visible tool errors. There is no automatic model fallback or unexpected billable request.
Authentication and headers come from PiG's model registry. Explicit auth headers remain authoritative.
OpenAI reasoning effort follows the live session setting and the `pi-ai@0.80.3` supported-level clamp.
Search does not change the active conversation model.

SSE updates stream to the tool surface. Results include cited answers, sources, queries, native-search calls,
search-result metadata, and URL retrieval status. Unicode citations use Gemini byte offsets or OpenAI UTF-16 offsets.
Provider POST redirects are refused. Google grounding redirects use unauthenticated, bounded HEAD requests;
a failed optional resolution retains its original URL and records a warning.
The request deadline is 90 seconds. Limits are 32 MiB per stream, 1 MiB per event, 8 MiB of answer text,
256 result/citation/call/query entries, and bounded metadata depth and size.
Unlike upstream, incomplete terminal responses and streams without a terminal response are errors, not partial success.
Cancellation remains an error even during grounding resolution.

The four transport fixtures were replayed through the original TypeScript adapters and formatter.
Go output matches cited text, sources, native-search status and query lists.
Mock-provider PiG execution also verifies explicit search-model configuration, discovery through the real `builtin:tool-search`, and Gemini-only tool suppression.
Live paid-provider calls were not used for verification.
Do not enable the TypeScript web-search twin at the same time.

## ask-user-question

The native Go extension ports `@juicesharp/rpiv-ask-user-question@2.12.0`.
`ask_user_question` uses the original JSON schema: 1–4 questions, 2–4 options per question,
headers of at most 16 UTF-16 units, and option labels of at most 60 UTF-16 units.
Carriage-return normalization, reserved labels, duplicate checks, answer details, and response envelopes follow upstream.

The TUI has question tabs, a review/submit tab, checkbox multi-select, free text, option previews,
per-question notes, global notes, partial submission, and cancellation with partial answers preserved.
Arrow keys select options; Enter confirms; Tab/Shift+Tab switch tabs outside text entry.
`n` opens notes outside text entry. Shift+Enter adds a line; Ctrl+U clears a custom draft.
Ctrl+G opens PiG's multiline editor dialog, not the system `$EDITOR`.
Escape closes notes or cancels the questionnaire.
Ctrl+] collapses the questionnaire to a visible hint row; the same key expands it.

RPC uses upstream's sequential select/input fallback, including numeric multi-select and custom answers.
Print and other non-interactive calls return `no_ui` without opening a dialog.
The tool is model-only, not available through codemode.
Unlike upstream's visibility reconciliation, it remains registered in print mode:
PiG removes inactive tools from execution, which would replace the required no-UI error with “Tool not found.”

Configuration lives at `$XDG_CONFIG_HOME/rpiv-ask-user-question/config.json`, or
`~/.config/rpiv-ask-user-question/config.json`. A missing XDG file falls back to the legacy path.
Malformed primary files warn and use defaults instead of falling back. The file limit is 1 MiB.
`guidance` can override `description`, `promptSnippet`, and `promptGuidelines`.
`collapseKey` accepts a key specification or `"off"`.

Approved host limits: default semantic keys rather than custom host keybinding resolution;
visible collapse rather than a hidden overlay with raw-input reopening.
Previews use bounded plain-text/code rendering, side-by-side at 100 columns or wider and stacked otherwise.
They do not reproduce upstream's full Markdown styling. Narrow terminals use conservative Unicode cell counts.

Fixtures replay normalization, validation precedence and response envelopes through the pinned TypeScript.
Real PiG runs cover RPC answers/cancellation and print-mode rejection.
A real 120-column TUI smoke exercised previews, collapse/expand, tabs, multi-select, custom input,
question/global notes, editor-dialog cancellation with draft restoration, review, submission and clean exit.
Do not enable the TypeScript questionnaire twin at the same time.

## todo

The native Go extension ports `@diegopetrucci/pi-todo@0.1.11`.
`todo` supports `list`, `add` with `text`, `toggle` with numeric `id`, and `clear`.
Each result includes the original `{action, todos, nextId, error?}` snapshot.
PiG stores these details in its session JSONL; there is no separate task file.
Session start and tree navigation restore the latest full snapshot on the active branch.
State stays inside the extension factory. New-session replacement starts with an empty list and ID 1.
Tool batches run sequentially; a mutex also protects state from concurrent internal callers.

Tool cards preserve status markers and the five-item collapsed list; expanded cards show all items.
`/todos` opens the original read-only list with a completion count and closes on Escape or Ctrl+C.
It requires the TUI, not RPC or print mode. No persistent widget is installed.
Terminal controls are removed from rendered text. Long tool-card text is limited to 1,024 wrapped lines per item.
Malformed latest snapshots fail visibly and block mutations rather than falling back to stale state.
The ID counter stays inside JavaScript's exact-integer range.

Eighteen actions and branch/resume snapshots were replayed through the pinned TypeScript.
Real PiG runs verify persisted resume, tree navigation to an earlier todo result, branch ID reuse,
clear/reset, and factory replacement on a new session.
A native TUI smoke verifies sequential batch IDs, themed cards, the collapsed list,
all seven items in `/todos`, Escape closure and clean exit.
Do not enable `harness-interact` or the TypeScript todo twin at the same time.
