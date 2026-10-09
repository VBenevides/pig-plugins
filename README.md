# pig-plugins

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/VBenevides/pig-plugins/main/scripts/install.sh | sh
```

Requires `git`, `go`, and `curl`. If `pig` is not on `PATH`, the script first installs PiG 0.4.1
(the version the bundled patches target; override with `PIG_VERSION`) into `~/.local/bin`.
The script clones the repository into a temporary directory and builds the fused executable at `~/.pig/bin/pig-plugins`.
Only after that build succeeds does it copy the plugins, prompts and skills into `~/.pig` and remove the clone. Details are under
[Install into `~/.pig`](#install-into-pig). Set `PIG_PLUGINS_REPO` to install from another clone or fork.

## About

Native Go extensions for [PiG](https://github.com/MichaelKinsy/PiG), including the Go port of
`pi-image-view`. Each native folder under `extensions/<name>/`
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
On quota failure, auto-models first tries another native account for the same provider with at least 5% known quota remaining, retaining the model. Only if none is eligible does it try the configured fallback. Account or model recovery shares one continuation budget per interrupted task; explicit `--model` disables both. Background low-quota rotation still waits until the session is idle.
In `pig -e` mode PiG hides host CLI arguments, so switching is disabled with a warning. `/usage` and `/auto-model` still work.
Differences from upstream: only OAuth credentials are used, quota requests have a timeout, size limit, and no redirects, and persistence errors are reported, not ignored.
Native account storage and quota integration received a read-only security review with no material findings.

### better-footer

Bundled development binaries now include `pi-better-footer@0.1.3`'s native port.
The footer shows interaction throughput: cumulative model output tokens divided by
elapsed wall time since `agent_start`, including model waits and tool execution.
Tool-result tokens do not count. `~` marks streamed estimates until reported usage
arrives; the final `t/s` is held after `agent_end` until the next interaction.
It also shows session usage, context, cost, cwd, Git branch/change counts, project
version, model, thinking, and quota.
Narrow terminals hide throughput first to keep the model visible.
Auto-models badges show `🧠 primary` or `⚡ fallback`; the model name appears only
in the footer's model field.
The `smart-approve-lancet` and `pi-curator` badges share a third footer row, shown only when one is set.
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
Submitting a prompt that holds `/skill:name` tokens adds each known skill's block once, before the unchanged text. Commands still work only at the start of the message.
`patches/pig/0006-scrollable-extension-dialogs.patch` limits the title and description of a select dialog (such as the smart-approve prompt) to 12 rows. PageUp and PageDown scroll the rest, and a status row shows the visible range.

`patches/pig/0007-dialog-page-keys.patch` lets an open dialog receive PageUp and PageDown before the full-screen chat viewport, which used to consume them.
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


### Numbered images (native Go)

The Go port of [pi-image-view@0.4.0](https://github.com/alchemistklk/pi-image-view)
numbers image paths, RPC attachments, and native `read` results using `[Image #N]`.
PNG, JPEG, GIF (first frame), and WebP decode in Go. Submitted PNG previews are bounded
to 480 pixels and 2 MiB; `/pi-image-view detail` arms the next submission for 1280 pixels.
Input decoding is bounded to 32 MiB and 24 million pixels.
`/pi-image-view clear` removes earlier images from future model context without changing history.
Numbering resumes from user and tool-result messages on the current session branch.
Prepared attachments persist atomically under `<agent-dir>/image-view/blobs/`, keyed by SHA-256.
Display links resolve to local blobs; internal blob links are stripped from model-facing text.
Existing native `read` and `edit` tools remain active.

The native UI keeps PiG's editor and clipboard handling. In the patched bundle, pasted image
paths automatically become `[Image #N]` markers before submission, with a gallery above the editor.
Previews use PiG's native Kitty or iTerm2 graphics; other terminals use color-cell thumbnails.
Images appear side by side (up to four per row), wrapping at narrow widths. Draft preview inputs
retain up to 1280 pixels independently of the 480-pixel default model attachments.
Draft scans run every 100 ms and replace text only if it is still unchanged.
Up to 16 draft attachments are retained; deleting a marker removes its pending attachment.
Submission preserves marker numbers and applies detail mode to the retained original image.
The draft gallery clears after image submission; submitted image blocks remain in the transcript.
Use `/pi-image-view preview PATH` for an explicit preview; command and shell drafts are not converted.
Atomic marker editing is not ported. Standalone source extensions on unpatched PiG retain
submission handling but require the patched host and SDK for the draft gallery.
PiG still renders submitted image blocks using its normal terminal-image renderer.
The MIT license remains in `extensions/pi-image-view/LICENSE`; pinned TypeScript source
is retained only as reference material in `testfixtures/image-view/upstream/`.
The fused bundle contains only Go extensions and needs no Node.js or npm runtime.

#### Image settings and terminal support

Use these settings in the normal PiG agent directory's `settings.json`:

```json
{
  "terminal": { "showImages": true, "images": "auto" },
  "images": { "autoResize": true, "blockImages": false }
}
```

These are the recommended image settings.
Use a graphics-capable terminal, such as Kitty or Ghostty, for sharp previews.
The native gallery uses the host's configured Kitty or iTerm2 renderer; Unicode placeholders
are not required. With no graphics protocol, all images still appear as color-cell thumbnails.
PiG disables iTerm2 graphics in fullscreen mode; use `tuiMode: "regular"` for iTerm2 previews.
Print, JSON, and RPC modes retain numbered references and model attachments, but do not display the draft gallery.

PiG disables automatic image protocol detection inside tmux and screen.
The native gallery follows the host renderer and does not add multiplexer passthrough.
Use PiG outside the multiplexer for reliable previews. Force `terminal.images: "kitty"` or
`PI_IMAGE_PROTOCOL=kitty` only when the terminal and multiplexer can forward the graphics protocol.
The setting takes precedence over the environment variable.
Use PiG outside the multiplexer if forwarding fails.
Stock `showImages` and `blockImages` settings control the transcript, not the draft gallery.
Do not use `blockImages` as a privacy boundary: PiG 0.4.1 still sends those images to the model.

#### Host smoke test

1. Build the mixed-language development binary:

   ```sh
   binary=$(./scripts/dev_build.sh)
   ```

2. Run the deterministic host integration test:

   ```sh
   PIG_IMAGE_SMOKE_BINARY="$binary" go test ./testfixtures/image-view -run TestBundledImage -count=1 -v
   ```

   This test starts the actual bundled host with an isolated HOME and a local mock model.
   It checks native Go commands, pathless image input, image paths, native image reads, and sequential references.
   It also checks model attachments, actual 480-pixel resizing, and absence of duplicate tools.
   On Linux with Python 3, it also drives the real editor in a pseudo-terminal and checks
   that pasted paths and Ctrl+V images become markers and show side by side before submission.
   It then submits immediately after typing and checks that both images reach the model request.
   The smoke fixture checks Kitty, iTerm2, color-cell fallback, and narrow-width resizing.
   Without `PIG_IMAGE_SMOKE_BINARY`, the test skips. A skip is not smoke-test evidence.

3. Start `"$binary"` in a Kitty-compatible terminal.
4. Paste two images and expect `[Image #N]` markers with sharp previews side by side above the editor.
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

Requires `pig` and Go on `PATH`. Run:

```sh
binary=$(./scripts/dev_build.sh)
"$binary"
```

The script builds `build/pig-plugins` for the current platform and prints its absolute path to stdout.
Installation and build stages are shown live on stderr, including PiG's fused-build progress.
Go compilation defaults to two parallel jobs to reduce desktop memory and disk pressure.
Override with `PIG_BUILD_JOBS=4`; `GOMAXPROCS` is also defaulted to 2 unless already set.
Source staging excludes checkout-local `.git`, `.agent-work`, `build`, `node_modules`, and `.ouro` directories.
Build diagnostics go to stderr. To choose another output path:

```sh
./scripts/dev_build.sh ./build/pig-plugins-custom
```

Relative output paths are relative to the invoking directory; the script can run from outside the repository.
Successful builds replace an existing output binary. A failed build leaves the previous binary intact.
`piglet.yaml` bundles all selected native Go extensions and disables ambient extension and skill discovery; a baked binary ignores ambient discovery entirely. The `user-resources` extension loads skills, prompts and themes from `~/.pig/agent` instead.
The executable does not need Go, Node.js, or the source tree to run.
It uses normal PiG model selection and credentials.
Curator, language servers, and the optional LANCET model and ONNX Runtime library remain external prerequisites.
The script does not install extensions into your default configuration.

The first build downloads the source and dependencies for the installed PiG release.
It uses a temporary workspace to resolve the extensions' dependency checksums and placeholder SDK version.
The script applies the pinned host and SDK patches, then builds a temporary patched builder.
Both the builder and the output binary use the native host patches; no Node runtime repacking is needed.
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

The safety layer: it registers no tool and gates `bash`, `write` and `edit` through `tool_call`. It is on by default.
Use `/smart-approve-lancet off` to bypass all guard checks, or `/smart-approve-lancet on` to restore them.
The footer shows `smart-approve-lancet on - auto/interactive - lancet on/off` while enabled, and only
`smart-approve-lancet off` while disabled. Both switches and the mode persist across sessions.
While enabled, hard-blocked bash behaviors (`rm -rf /`, `curl | sh`, fork bombs, ...) are always blocked.
Other dangerous behaviors and protected paths (`~/.ssh`, `.env`, ... with `.env.example` allowed; symlinks are
resolved, dangling ones included) ask for confirmation in `interactive` mode and are blocked in `auto` mode
or when no UI exists. `auto` is the renamed `strict` mode with unchanged behavior; legacy settings still load.
With `/smart-approve-lancet lancet on`, commands outside the verified read-only subset are scored locally first:
`risky` is blocked, `review` asks, `not_flagged` continues, and an unavailable model blocks.
A damaged settings file (`smart-approve-lancet.json` in the agent directory) fails closed and is reported.
Commands: `/smart-approve-lancet [on|off|interactive|auto|status]` (no argument toggles the mode) and
`/smart-approve-lancet lancet [status|setup|on|off|check <command>]`.

Verified read-only commands (`readlink`, `strings`, `rg`, `grep`, `head`, `tail`, `cat`, `ls`, `pwd`, `wc`, `stat`)
bypass LANCET scoring and approval, including pipelines and lists made entirely from these commands.
Redirects, substitutions, executable hooks, wrappers, and mixed read/write commands retain the existing policy.
`rg` with `RIPGREP_CONFIG_PATH` set also retains scoring because its config can supply executable hooks.
Hard-block rules still take precedence.

The confirmation shows `Affected items:` with one `- type: name - effect` entry per known target. Simple commands
name deleted files/folders and explicit Git branches/tags; protected writes name the file and the write/edit effect.
Implicit targets, shell expressions, compound commands and unsupported options show an unknown scope instead of
guessing. For an unknown-scope confirmation, the current session model receives only the command and working
folder to explain risks and possible affected items (an additional provider request that can incur API costs).
Its assessment is labeled advisory, never executes tools, and keeps the static unknown-scope warning. Failure or
invalid output shows an unavailable notice and still requires approval. Known targets, stored grants, strict/no-UI
blocks and hard blocks do not request an assessment. Requests cap output at 1024 tokens, disable retries and set
a 15 s provider timeout (provider timeout semantics apply). This display does not change approval rules or saved
grant keys. The dialog answers `Deny`, `Allow once`
or `Always allow this command and item`. The last stores the exact command (or the tool, for `write` and `edit`)
together with its policy item (delete paths or the working folder for bash; the file path for write/edit) in
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
- LLM analysis is advisory and limited to unknown-scope confirmations; there is no `auto` mode.
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

The native Go extension ports [code-yeongyu/pi-todotools](https://github.com/code-yeongyu/pi-todotools)
at commit `50b85f7e39c94a8fa8253eb3628515f188a42a1c`, whose phased model derives from Oh My Pi v17.0.5.
It replaces the old `action`/numeric-ID API. Tasks are `{content, status}` with
`pending`, `in_progress`, `completed`, or `abandoned` status.

| op | Fields | Effect |
|---|---|---|
| `init` | `list: [{phase, items}]` or flat `items` | Replace the full list |
| `start` | `task` | Select active work; demote the previous active task |
| `done` / `drop` | `task` or `phase` | Complete / abandon work |
| `append` | `phase`, `items` | Add tasks; create the phase if missing |
| `rm` | optional `task` or `phase` | Remove tasks; omit both to clear all |
| `view` | none | Read the list without mutation |

```json
{"op":"init","list":[{"phase":"Implementation","items":["Update the implementation","Add regression tests"]},{"phase":"Verification","items":["Run focused checks"]}]}
{"op":"done","task":"Update the implementation"}
{"op":"view"}
```

Pass **verbatim task content** or phase names, never IDs. New tasks must be globally unique.
When no task is active after a mutation, the earliest pending task auto-promotes in phase order.
Completing later work never reopens completed tasks. With no target, `done`/`drop` affect all tasks;
prefer explicit targets. With both targets, `task` takes precedence. `rm` leaves empty phase containers.

Successful mutations persist `{schema:"v2", phases}` in `sanepi.todo-state` session entries before
updating memory. View and validation failures write nothing. Tool details contain
`{op, phases, storage, completedTasks?}`. Session reload/tree navigation reads the latest valid
branch-local custom entry or historical `todo`/`todowrite` result. Old flat upstream lists and this
repository's old ID/text/done snapshots migrate automatically; unknown legacy statuses become
pending and cancelled becomes abandoned. Invalid snapshots produce warnings and retain the last valid
state, matching upstream fallback rather than the old port's mutation blocking.
Legacy duplicate content is preserved; exact-content targeting selects the first match, as upstream.
Use init with unique task text to disambiguate such lists. New sessions start empty.

The live active-phase widget appears **above the editor** through PiG's normal string-array widget
API (ten-row cap and host-managed wrapping/resizing), not a separate side pane. It hides when all
tasks are completed or abandoned. A widget failure warns without undoing an already-durable mutation.
Cards show Roman-numbered phases and themed status markers; collapsed cards summarize untouched
phases, while expanded cards show every phase. Terminal controls are removed, and cards are capped
at 1,024 wrapped lines. `/todos` opens the full read-only phased viewer with a done count;
Escape or Ctrl+C closes it. `/todos` requires TUI mode; the tool works in RPC/print mode.

The extension adds task-management guidance once per effective system prompt.
Usage examples also live in [`prompts/agent/SYSTEM.md`](prompts/agent/SYSTEM.md).
It does not read or write a workspace TODO.md. Markdown conversion helpers are internal library
functions, not disk import/export commands. Ask mode continues to permit session task notes.

Tests cover operations, atomic rejection, migration, persistence rollback, active-phase rendering,
Markdown conversion, and idempotent guidance. Real PiG tests exercise resume, branch/new-session
isolation and successful-mutation-only custom entries. A 120-column TUI smoke exercised phase
progression, `/todos` 3/3 status, Escape closure and clean exit.
Do not enable another todo extension at the same time.

## project-prompt

The bundled native extension loads project-specific instructions on `before_agent_start` and preserves the entire host prompt.
The global prompts (`SYSTEM.md`, `APPEND_SYSTEM.md`, `AGENTS.md`) are the ones in `prompts/agent/`; the installer copies them to the agent directory.
For trusted projects, it also loads the literal `.local/APPEND_SYSTEM.md` and
`local/AGENTS.md` paths from the current working directory on each request.
Missing files are optional; content already loaded by the host is not repeated.
Files must be regular UTF-8 files within the project, at most 256 KiB each.
`local/APPEND_SYSTEM.md` is not substituted for `.local/APPEND_SYSTEM.md`.

## ask-mode

`/ask` toggles read-only mode; `/ask on`, `/ask off` and `/ask status` set or show it.
The state is in memory only and resets to off when a session starts.
While on, the footer shows `ASK`, the system prompt forbids any change, and `tool_call` blocks everything except read-only tools and provably read-only bash (no redirection, substitution or write-capable commands).
`write` and `edit` are allowed only inside `<cwd>/.agent-work/` (symlinks resolved) and only after you confirm each one.

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
