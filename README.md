# pig-plugins

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/VBenevides/pig-plugins/main/scripts/install.sh | sh
```

Requires `git`, `go`, Node.js 22.13 or newer, and `curl`. If `pig` is not on `PATH`, the script first installs PiG 0.4.1
(the version the bundled patches target; override with `PIG_VERSION`) into `~/.local/bin`.
The script clones the repository into a temporary directory and builds the fused executable at `~/.pig/bin/pig-plugins`.
Only after that build succeeds does it copy the plugins, prompts and skills into `~/.pig` and remove the clone. Details are under
[Install into `~/.pig`](#install-into-pig). Set `PIG_PLUGINS_REPO` to install from another clone or fork.

## About

Go extensions for [PiG](https://github.com/MichaelKinsy/PiG) (the Go port of Pi), plus the original
Node/TypeScript `pi-image-view` extension. Each native folder under `extensions/<name>/`
exports `func Extension() *sdk.Extension`. The repository uses one Go module for shared `internal/` packages.

Upstream authors, repositories and licences for the ported extensions are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Port status

Implemented: `hashline-edit`, `smart-approve-lancet`, `pi-curator`, `lsp`,
`web-search`, `ask-user-question`, `todo`, `auto-models`, and `better-footer`.
Use `pig -e ./extensions/<name>` to load a port for one session.
Validation with `make check` does not enable these extensions in the default configuration.

`rewind` is deferred by user choice.

### auto-models

Port of `pi-auto-models@0.1.14`. It adds `/usage` (Claude and Codex quota windows) and `/auto-model` (primary and fallback model, with thinking level).
In a TUI, `/usage` opens a scrollable dashboard (`q`, `esc` or ctrl-c closes; `j`/`k`, arrows and page keys scroll). Other modes print a notification.
The footer quota badge shows the active provider only. The `openai-codex` provider, and `openai` when its token carries a ChatGPT account id, use the ChatGPT quota endpoint.
An `openai` API-audience OAuth token is rejected by that endpoint (401), so it is not queried; its quota appears only from response headers after use.
`/auto-model` offers only the session's scoped models (`--models` or `enabledModels`). With no scope it offers every authenticated model.
Settings and caches live in the agent dir: `auto-model.json`, `claude-quota-cache.json`, `auto-model-rate-limits.json`. The patched native host stores OAuth accounts in `oauth-accounts.json` and lazily migrates existing singleton OAuth logins from `auth.json`.
Startup model selection and 429/529 fallback run only in a fused binary (see `scripts/dev_build.sh`) and only without an explicit `--model`.
In `pig -e` mode PiG hides host CLI arguments, so switching is disabled with a warning. `/usage` and `/auto-model` still work.
Differences from upstream: only OAuth credentials are used, quota requests have a timeout, size limit, and no redirects, and persistence errors are reported, not ignored.
Native account storage and quota integration received a read-only security review with no material findings.

### better-footer

Bundled development binaries now include `pi-better-footer@0.1.3`'s native port.
The footer shows generation speed (`~` for streamed estimates, final `t/s` excluding
time to first token and reported reasoning tokens), session usage, context, cost,
cwd, Git branch/change counts, project version, model, thinking, and quota.
Narrow terminals hide throughput first to keep the model visible.
Auto-models badges show `🧠 primary` or `⚡ fallback`; the model name appears only
in the footer's model field.
`/better-footer` toggles recent-model persistence and skipping confirmed exhausted
providers during scoped model cycling. Settings live in `better-footer.json`.
Explicit CLI selections and resumed sessions take precedence over restoration;
`--no-recent-model` disables restoration and recording for that run.
Non-fused `pig -e` sessions disable recent-model tracking because CLI precedence
cannot be determined. Auto-models and approval badges are preserved through a
shared status mirror in fused builds; unrelated subprocess extensions cannot
publish their badges to this mirror.

Quota sources include selected native Codex account HTTP usage, Z.AI, OpenCode Go,
Copilot, and response headers. Codex CLI app-server is only a fallback when no
native account exists. Native account cache keys prevent one account's balance
from being attributed to another.

### Native multi-account Codex

The bundled build applies pinned PiG 0.4.1 host and SDK patches under `patches/pig/`.
It adds durable account selection and independent quota reporting.
Start `build/pig-plugins`, run `/login openai-codex` once for each account,
then use `/accounts` to choose the account used for native model requests.

`patches/pig/0004-mid-prompt-skill-autocomplete.patch` lets you type `/` after prompt text to complete a skill, as in Oh My Pi.
The popup lists skills only and matches the `skill:` prefix, a name prefix, or a hyphen-separated name segment. Accepting inserts `/skill:name` and does not submit.
`patches/pig/0005-node-editor-mid-prompt-skill-autocomplete.patch` does the same for the Node editor that `pi-image-view` installs in place of the host editor.
Submitting a prompt that holds `/skill:name` tokens adds each known skill's block once, before the unchanged text. Commands still work only at the start of the message.
`patches/pig/0006-scrollable-extension-dialogs.patch` limits the title and description of a select dialog (such as the smart-approve prompt) to 12 rows. PageUp and PageDown scroll the rest, and a status row shows the visible range.
Select `openai-codex/gpt-6.1-sol` with `/model`; `/usage` shows the actual active
model plus every native OAuth account, with labels, selection markers, and
individual quota windows. A failed account does not hide successful neighbors.
The `openai` direct API login remains distinct from `openai-codex`; its token
does not supply a numeric ChatGPT quota.

OMP's accounts in `~/.omp/agent/agent.db` are separate and are not copied.
Both existing OMP Codex accounts must be logged into PiG before this dashboard
can report them. Expired credentials are reported without silently refreshing
or changing account selection merely to display usage.
Run `make gowork` to use the patched SDK for development. Host tests requiring
upstream TypeScript comparison packages cannot run unless those packages are
installed; targeted native lifecycle, refresh, cancellation and bridge tests
are covered independently.


### Numbered images (Node, not a Go port)

The development binary includes [`pi-image-view@0.4.0`](https://www.npmjs.com/package/pi-image-view/v/0.4.0),
the [alchemistklk fork](https://github.com/alchemistklk/pi-image-view) of
[RielJ/pi-image-preview](https://github.com/RielJ/pi-image-preview).
It replaces pasted image paths with stable `[Image #N]` references.
The original extension builds 480-pixel PNG thumbnails for the draft gallery and model attachments.
`/pi-image-view detail` requests a 1280-pixel image batch.
`/pi-image-view clear` removes earlier images from future model context, but preserves session history.

The package source and MIT license are under `extensions/pi-image-view/`.
`upstream-lock.json` records the exact npm archive, version, and SHA-512 integrity.
No `node_modules` directory or npm installation is required.
PiG's Node loader supplies the package's peer APIs and Photon WASM image resizer.
The small host adapter numbers native `read` image results and pathless RPC image inputs.
It also restores numbering from saved user and tool-result messages.
The blob store uses PiG's agent-directory API instead of the original `~/.pi/agent` fallback.
Submitted previews persist under `<agent-dir>/image-view/blobs/`.
All other package files retain the published implementation.
The extension registers no tools. The existing native `read` and `edit` remain active.

The binary mixes fused Go extensions with one Node subprocess.
PiG derives each extension's realization from its language. The manifest does not accept a per-extension runtime declaration.
Node.js 22.13 or newer must remain on `PATH` when you build and run this binary.
The build applies `patches/pig/0003-node-piglet-source-cells.patch` to PiG 0.4.1.
This patch records the Node runtime requirement and embeds the extension source with its relative imports.
At startup, PiG extracts those files and uses its existing Node loader.
The runtime comes from `PATH`, not from the binary.

#### Image settings and terminal support

Use these settings in the normal PiG agent directory's `settings.json`:

```json
{
  "terminal": { "showImages": true, "images": "auto" },
  "images": { "autoResize": true, "blockImages": false }
}
```

These are the recommended image settings.
The upstream draft gallery uses Kitty graphics and Unicode placeholders.
Use a terminal that supports both, such as Kitty or Ghostty.
The upstream gallery shows text labels instead of thumbnails in other terminals, including iTerm2-only terminals.
PiG's stock image renderer can separately use the iTerm2 protocol.
Print, JSON, and RPC modes retain numbered references and model attachments, but do not display the draft gallery.

PiG disables automatic image protocol detection inside tmux and screen.
The upstream gallery can detect a Kitty-capable outer terminal and emit tmux passthrough sequences.
This requires working passthrough and Unicode-placeholder support in the multiplexer and outer terminal.
For a known-compatible setup, use `terminal.images: "kitty"` or `PI_IMAGE_PROTOCOL=kitty`.
The setting takes precedence over the environment variable.
Use PiG outside the multiplexer if forwarding fails.
Stock `showImages` and `blockImages` settings control the transcript, not the upstream gallery.
Do not use `blockImages` as a privacy boundary: PiG 0.4.1 still sends those images to the model.

#### Host smoke test

1. Build the mixed-language development binary:

   ```sh
   binary=$(./scripts/dev_build.sh)
   ```

2. Run the deterministic host integration test:

   ```sh
   PIG_IMAGE_SMOKE_BINARY="$binary" go test ./testfixtures/image-view -run TestBundledImageView -count=1 -v
   ```

   This test starts the actual bundled host with an isolated HOME and a local mock model.
   It checks Node and Go commands, pathless image input, image paths, native image reads, and sequential references.
   It also checks model attachments, actual 480-pixel resizing, and absence of duplicate tools.
   Without `PIG_IMAGE_SMOKE_BINARY`, the test skips. A skip is not smoke-test evidence.

3. Start `"$binary"` in a Kitty-compatible terminal.
4. Paste an image path and wait for `[Image #1]` and its thumbnail above the editor.
5. Submit the image, then ask the model to read another PNG with `read`.

   Expect the next numbered reference and an inline image result, without another `read` tool.
   Use `/pi-image-view detail` before the next image to inspect small text.
   A headless test cannot prove that terminal pixels display correctly.
   Record the terminal, image protocol, multiplexer, and visible result when you run this TUI check.

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

### Install into `~/.pig`

```sh
./scripts/install.sh   # or: make install
```

Copies (no symlinks) the tracked repository, including every extension and `prompts/`, to `~/.pig/pig-plugins`.
Copies `prompts/agent/SYSTEM.md`, `prompts/agent/APPEND_SYSTEM.md` and `prompts/agent/AGENTS.md` into `~/.pig/agent/`; an existing different file is first saved as `<file>.pig-plugins-backup-<timestamp>`.
Copies every `skills/<name>/` directory that holds a `SKILL.md` into `~/.pig/agent/skills/`, including untracked ones. A different existing skill moves to `~/.pig/agent/skills-backup/<timestamp>/<name>`; skills not in this repository are left alone.
Builds the fused executable at `~/.pig/bin/pig-plugins` and symlinks it as `~/.local/bin/pig-plugins` (`PIG_PLUGINS_LINK_DIR` overrides the directory; an existing non-symlink is left alone). Run it instead of `pig`.
`PIG_HOME` and `PIG_CODING_AGENT_DIR` override the locations. Re-running replaces the copy.

### Build a bundled development binary

Requires `pig`, Go, and Node.js 22.13 or newer on `PATH`. Run:

```sh
binary=$(./scripts/dev_build.sh)
"$binary"
```

The script builds `build/pig-plugins` for the current platform and prints its absolute path to stdout.
Build diagnostics go to stderr. To choose another output path:

```sh
./scripts/dev_build.sh ./build/pig-plugins-custom
```

Relative output paths are relative to the invoking directory; the script can run from outside the repository.
Successful builds replace an existing output binary. A failed build leaves the previous binary intact.
`piglet.yaml` bundles all nine native extensions plus `pi-image-view` and disables ambient extension and skill discovery; a baked binary ignores ambient discovery entirely. The `user-resources` extension loads skills, prompts and themes from `~/.pig/agent` instead.
The executable does not need Go or the source tree to run. Its image subprocess needs Node.js.
It uses normal PiG model selection and credentials.
Curator, language servers, and the optional LANCET model and ONNX Runtime library remain external prerequisites.
The script does not install extensions into your default configuration.

The first build downloads the source and dependencies for the installed PiG release.
It uses a temporary workspace to resolve the extensions' dependency checksums and placeholder SDK version.
The script applies the pinned host and SDK patches, then builds a temporary patched builder.
Both the builder and the output binary use the Node source-cell fix.
Cached source and repository Go files are not modified; the temporary source copy is removed on exit.
For a development PiG checkout, set `PIG_SOURCE_ROOT` to its absolute path before running the script.
Build artifacts are ignored by Git.

## Parity

Each extension ports an existing TypeScript original. Parity tests compare Go output with golden files captured
from that original (`testdata/` or `testfixtures/`). Approved host limits and intentional safety differences are
listed in each extension's section. `pigtest.Golden` has no update flag: Go output must not regenerate its own baseline.

Do not enable a Go port together with its TypeScript twin: PiG rejects duplicate tool names.

## hashline-edit

Replaces the built-in `read` and `edit` with anchored read (`<line>#<hash>|text`, four hex digits of SHA-1) and strict
anchored edit. It does not match the upstream npm `pi-hashline-edit`
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

The confirmation shows the affected paths: the targets of an `rm -rf`, or the working folder when a command does not
name them; a protected write shows its folder. The dialog answers `Deny`, `Allow once` or `Always allow this command
and item`. The last stores the exact command (or the tool, for `write` and `edit`) together with its affected item in
`smart-approve-lancet-allow.json` in the agent directory (mode 0600); only that pair is skipped later, and the other
sessions see it at once. `strict` mode, hard blocks and LANCET `risky` verdicts still block. A damaged allow list
grants nothing and is reported on stderr. A recursive force delete whose every target is known and strictly inside
`<cwd>/.agent-work` or any `tmp` folder below the working folder (symlinks followed) runs without asking, in every
mode; so does a protected-path write inside those folders.

The pattern tables are a port of the `behaviors.ts` and `paths.ts` of `smart-approve`;
`internal/guard/testdata/ts-golden.json` holds the TypeScript verdicts for 402 commands, 93 delete targets, 27
normalizer inputs and 81 paths (`.agent-work/scripts/smart-approve-golden/generate.mjs` regenerates it). JavaScript
regexps run in `regexp2` with its ECMAScript mode, a 2 s match timeout, and `\s` widened to the JavaScript whitespace set.

Differences from the TypeScript original:

- Block reasons start with `smart-approve-lancet: `.
- No LLM risk analysis and no `auto` mode.
- `.` in a path glob also matches a line break, so a newline in a path cannot dodge a `**` pattern.
- The ONNX Runtime library is not bundled. `lancet setup` downloads the pinned model (LANCET Nano v0.4.3) and the pinned
  ONNX Runtime 1.30.0 archive for linux/amd64, linux/arm64 or darwin/arm64 into `<agent dir>/smart-approve-lancet/`;
  both are checked for size and SHA-256 before use, and the library is hashed again before every load. Set
  `LANCET_ORT_LIBRARY` to use another library (and on other platforms).
- A native scoring call cannot be interrupted once it started; scoring is bounded by a 60 s timeout between steps.

Do not enable another smart-approve extension at the same time.

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

`/pi-curator [status|off|on|prefetch <off|64..8192>|startup <off|1..10>|engine <legacy|fts|hybrid|episodes|state>]`
stores settings in `<agent dir>/pi-curator.json` with mode 0600.
Saved settings take precedence over environment variables.
The integration defaults are prefetch 512, startup 2, and engine legacy.
Set `PI_CURATOR_PREFETCH_BUDGET=0` to disable task prefetch.
Set `PI_CURATOR_STARTUP_DECISIONS=0` to disable startup history as well.
`PI_CURATOR_SEARCH_ENGINE` selects the tool engine. `PI_CURATOR_PREFETCH_ENGINE` separately selects the prefetch engine.
`/pi-curator off` persists `enabled=false`. It stops capture, recall, consent prompts, and the memory tools at once, and survives restart.
`/pi-curator on` re-enables the extension. It does not grant consent.
Startup history and task recall share one budget, which includes the envelope. Startup history takes at most half when a task search runs. The "combined history exceeds prefetch budget" warning no longer occurs for normal budgets.
`prefetch off` disables task recall only. Startup history stays bounded at 512.
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
Do not enable another todo extension at the same time.

## project-prompt

The bundled native extension loads project-specific instructions on `before_agent_start` and preserves the entire host prompt.
The global prompts (`SYSTEM.md`, `APPEND_SYSTEM.md`, `AGENTS.md`) are the ones in `prompts/agent/`; the installer copies them to the agent directory.
For trusted projects, it also loads the literal `.local/APPEND_SYSTEM.md` and
`local/AGENTS.md` paths from the current working directory on each request.
Missing files are optional; content already loaded by the host is not repeated.
Files must be regular UTF-8 files within the project, at most 256 KiB each.
`local/APPEND_SYSTEM.md` is not substituted for `.local/APPEND_SYSTEM.md`.

## adhd-output

`/adhd on`, `/adhd off`, and `/adhd status` control optional presentation rules.
The default is off. State belongs to the current session branch; enabled rules
are invisible context messages, not extra model turns. The shared footer shows
`● ADHD ON`. See `extensions/adhd-output/README.md` for launch and configuration.
ADHD main points come from
[i-have-adhd revision 723af7d9afaf43eb871dbcce6129e2bf80de90d5](https://github.com/ayghri/i-have-adhd/blob/723af7d9afaf43eb871dbcce6129e2bf80de90d5/skills/i-have-adhd/SKILL.md);
the MIT notice is under `prompts/sources/`.

## repo-excludes

On trusted repository startup, this plugin adds missing `.agent-work/`, `.ouro/`,
and `.curator/` entries to Git's `info/exclude`, including linked worktrees.
It preserves existing bytes and permissions and never edits tracked `.gitignore`.
Failures are reported. See `extensions/repo-excludes/README.md`.

## cursor-login

Adds `cursor` to `/login`. It uses Cursor's browser PKCE flow (`cursor.com/loginDeepControl`, then polling
`api2.cursor.sh/auth/poll`) and refreshes through a `refresh_token` grant at `api2.cursor.sh/oauth/token`. These endpoints are undocumented
and may change. The extension only stores the OAuth credential; it registers no Cursor models, so Cursor
model requests are not yet supported.

## user-resources

Contributes `~/.pig/agent/skills`, `prompts` and `themes` (those that exist) at runtime, because a baked binary ignores
ambient discovery. Optional `~/.pig/agent/pig-plugins.json` changes this without rebuilding; a present key replaces its
default, relative paths resolve against the agent directory and `~/` is expanded:

```json
{"skillPaths": ["skills", "~/more-skills"], "promptPaths": [], "themePaths": []}
```

A malformed file is reported as an error. Which extensions are bundled is fixed at build time by `piglet.yaml`.
