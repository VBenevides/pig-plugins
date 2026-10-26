# Changelog

All notable changes to pig-plugins are documented here.

## [0.2.6] - 2026-10-26

### Features

- Bind Ctrl+Backspace to delete the previous word, like Ctrl+W.
- Move to the start of the first draft line with Page Up and the end of the last line with Page Down.

### Bugfixes

- Keep Up and Down from replacing a nonempty new draft with history. Preserve history recall from empty input and navigation through recalled messages.
- Deliver editor page keys to the focused editor in fullscreen mode instead of scrolling the transcript. Keep page navigation for open completion suggestions.

### Other

- Add editor and interactive-dispatch regression tests for word deletion, draft boundaries, history recall, and draft preservation.
- Include the editor navigation patch in native builds and patch-inventory checks. Document the text-input controls in the README.

## [0.2.5] - 2026-10-25

### Features

- Enable Kitty images inside tmux for attached Kitty, Ghostty, and WezTerm clients when `allow-passthrough` is `on` or `all`.
- Use Unicode placeholder cells and tmux passthrough to preserve image placement during scrolling, cropping, and cached rendering. Keep the text fallback when detection fails.

### Bugfixes

- Check user context limits before idle input starts a new run. Wait for compaction before the next question reaches the model.
- Keep active-run steering and follow-up input outside the compaction wait. Report compaction failures and let idle input continue.

### Other

- Apply the tmux image host patch in the fused build and include it in patch-inventory checks.
- Add regression tests for idle compaction success and failure, tmux detection, image transfer, placeholder rows, scrolling, and upload caching.

## [0.2.3] - 2026-10-24

### Features

- Add native Chrome and Firefox tools through `/browser-use mode off|chrome|firefox`, without Node. Support navigation, bounded page snapshots, clicks, text input, PNG screenshots, and tab selection.
- Reset browser mode to off on session start, reload, and `/new`. Hide disabled tools from discovery and provider declarations. Show the selected browser in the footer.
- Detect the installed browser and download, verify, cache, and start its driver on first use. Remove older drivers only after successful startup. Preserve external driver overrides and stop only owned driver processes.
- Preserve site logins in dedicated browser profiles. Add `hidden`, `login`, and `always` visibility modes, with direct browser login and explicit confirmation. Login mode returns to headless operation with the same profile.
- Run browser actions without per-action browser prompts while retaining host approval controls and ASK-mode restrictions. Restrict driver endpoints to loopback, retain TLS checks, reject password fills and file uploads, and never retry writes automatically.

### Other

- Add synthetic driver, real-host, footer, lifecycle, login, cancellation, cache, and security regression tests, plus optional live browser checks.
- Document browser setup, persistent profiles, visibility, safety limits, and OMP compatibility boundaries. Personal-profile attachment, OMP relay control, and arbitrary browser scripts are not supported.

## [0.2.2] - 2026-10-21

### Features

- Add user-invoked `/skill:code-analysis` for security and performance reviews. Reports include UTC/Git metadata, evidence, limitations, and verified numbered archives of previous reports.
- Add user-invoked `/skill:update-docs` for source-based README and documentation updates, architecture diagrams, and resource maps. The workflow commits only verified documentation.

### Bugfixes

- Install self-updates only when the remote plugins version is strictly newer. Equal or older remote versions leave the installation unchanged.
- Pin the remote version, installer, and checkout to one verified commit. Stop on lookup failures, invalid versions, prereleases, or checkout mismatches before installation.

### Other

- Bundle offline report, documentation, diagram, and commit formats for the new skills. Both skills require explicit invocation and stay hidden from automatic model discovery.
- Require STE writing rules and a writing review across all bundled skills. Preserve literal evidence, historical entries, code, commands, and required formats.
- Add regression coverage for version comparisons, update failures, metadata size limits, and pinned checkouts. Document the self-update checks and download limits.

## [0.2.1] - 2026-10-20

### Features

- Add shared Smart Approve modes: `off`, `lax` (the new-install default) and risk-based `strict`. Strict checks protected and unknown local read scopes as well as mutations; lax exempts verified reads.
- Persist the mode privately at `~/.pig/smart-approve-lancet/mode.json` using atomic replacement, independently of agent-profile overrides. Reload it for admissions and status requests; LANCET settings and saved grants remain profile-local.
- Order approval choices as `Allow`, `Deny`, `Always allow this command and item`. Allow is one-time only; cancellations and invalid responses deny.

### Bugfixes

- Reject stale admissions when the shared mode or scoring setting changes during approval or scoring, including changes from another running profile.
- Prevent unproven shell scopes, variables and wrappers from bypassing strict read checks. Corrupt or unreadable shared/profile settings fail closed rather than disabling checks.
- Reject obsolete or unadvertised dialog answers, including persistent approval when no grant store is available. Preserve damaged grant files when saving fails.

### Other

- Add mode/scoring, symlink, migration, storage-failure and concurrency tests, plus cross-process/profile and fused read/write consent coverage.
- Add real terminal coverage for choice order, default Enter, Deny, scrolling/resize and q/Escape/Ctrl+C cancellation. Update startup fixtures and document command migration and shared-mode storage.

### Breaking Changes

- Replace standalone Smart Approve on/off and interactive/auto selectors with `/smart-approve-lancet mode {off,lax,strict}`. The bare command now shows status. Legacy-only profiles use conservative strict checks until an explicit shared choice; old per-profile disabling is not promoted globally.
- Strict now means risk-based read/write checking, not the former automatic rejection of every flagged call. See the Smart Approve migration section in `README.md`.
- Approval dialogs initially select Allow, so Enter now allows once instead of denying. Saved grants still require the explicit Always allow choice.

## [0.2.0] - 2026-10-19

### Features

- Support PiG 0.5.1 with matching patched host and SDK integrations for native OAuth accounts, Cursor, editor input, dialogs, image previews, compaction and settings.
- Add `/goal resume` to continue stopped, blocked or idle restored goals without replacing their objective or saved progress. Busy agents, pending messages and repeated calls cannot queue duplicate continuations; reached or cleared goals cannot be resumed.
- Select matching host and SDK sources from the compatibility pin, independently of `pig` on `PATH`. Support explicit `PIG_BUILD_VERSION` candidate builds and isolate builder homes from user models, MCP servers and discovery settings.

### Bugfixes

- Wait for startup extension binding and already launched registration refreshes before RPC catalog discovery, preventing intermittent missing Cursor models. Preserve replay vetoes across streaming, persistence, retries and compaction.
- Restore image attachments after editor undo through a bounded payload cache without accepting submission-only stale image markers.
- Preserve settings submenu selections when submitting search input, including Kitty keyboard input.
- Adapt prompt consumers to lazy system-prompt resolution while preserving earlier contributions and propagating resolution errors. Retain diagnostic isolation, live output settings and ordered compaction on PiG 0.5.1.
- Keep distribution self-update routes owned by the pig-plugins installer after the upstream CLI relocation; explicit extension-only updates remain separate.

### Other

- Consolidate fifteen host/SDK patches into nine system-grouped artifacts, update build and workspace application order, and verify byte-identical resulting source trees.
- Expand real fused-bundle coverage for replay, goal resume, steering/queue delivery, literal commands, dialog cancellation and text ownership, images/undo, security boundaries, compaction/restart, raw-output recovery and diagnostics.
- Add build-source selection, installer preservation, updater route/failure/cleanup and patch-inventory checks. Formatting checks include tracked and unignored Go files without scanning ignored scratch trees.
- Document the 0.2.0 migration, qualification evidence and known upstream oracle/dependency limits in the README and `docs/pig-051-port.md`. Live external account and update testing was not performed.

### Breaking Changes

- PiG 0.5.1 and its matching patched SDK are required; older host compatibility is not promised. Rebuild with `pig-plugins update`.
- Go extension authors must migrate to the PiG 0.5.1 SDK, including typed OAuth credentials and lazy `before_agent_start.systemPrompt` reads through `Get()`, preserving errors and prompt composition.
- Custom model entries need an explicit `api` unless they replace a built-in. Library-only local packages without resource directories need `"pi": {}` to avoid extension discovery.
- Supplied boolean extension flags are always enabled. Replace `--adhd false` with `/adhd off` to save a disabled choice.

## [0.1.6] - 2026-10-19

### Bugfixes

- `pig-plugins update` now rebuilds the fused distribution through its installer instead of invoking PiG's upstream self-updater. `--update` remains an alias, including in development builds; explicit extension-only updates retain PiG's package-update behavior. Failed installer downloads are never executed, and installer failures remain visible.

### Other

- Installation now publishes a single fused executable at `~/.pig/bin/pig-plugins`, without a separate shell launcher or native payload. The executable supplies its host version and appends the managed dependency directory after existing `PATH` entries.
- Added regression coverage for legacy-payload migration, environment setup, installer failures and temporary-file cleanup, plus fused-binary smoke tests for self-update routes and extension-only updates.
- Updated installation and update documentation for the single-executable layout.

### Breaking Changes

- Successful installs remove the managed `pig-plugins-native` and `pig-plugins-raw` executables. Change scripts and shortcuts that use either legacy path to invoke `pig-plugins` instead.

## [0.1.5] - 2026-10-19

### Bugfixes

- Auto-models and better-footer now keep optional quota lookup failures out of warning popups, including expired credentials and usage API errors. Failures retain their original causes and sanitized debug diagnostics; model-selection warnings and successful account-switch notifications remain visible.

### Other

- Added regression coverage for quiet quota failures, preserved diagnostics and error causes, and account rotation past failed or expired neighboring accounts.

## [0.1.4] - 2026-10-19

### Features

- Changed hashline edits now renew authority for source fully displayed in their responses, including context lines. Subsequent edits can reuse the returned version and anchors without rereading. Previous coverage is discarded; shortened lines, omitted tails and summary gaps do not authorize replacement or deletion. Whole-file version checks, session isolation and bounded retention remain enforced.

### Other

- Added regression coverage for chained edits, coverage replacement, truncation, summary gaps, retention limits and authority revocation; extended golden assertions to cover edit-response display ranges.
- Updated tool descriptions, agent prompts and README guidance to distinguish reusable edit-response anchors from ranges that still require a read.

## [0.1.3] - 2026-10-18

### Features

- Added bundled, opt-in command-output reduction for Go test progress, Go test JSON and `go build -v`. Enable **Reduce command output** in `/settings`; changes persist and apply without restarting. Diagnostics remain visible, failed host results stay untouched, and commands, exit status and structured values are unchanged.
- Added private, bounded raw-output captures linked from reduced results, with oldest-first eviction and lifecycle cleanup. Recovery failures retain the original host output and show a warning; captures cannot restore bytes already truncated by the host. `PI_RTK_BYPASS=1` bypasses reduction.
- Added bundled, read-only `ast_search` using ast-grep, available through tool discovery and in ASK mode. Searches return bounded snippets and source locations, honor existing source discovery, isolate repository configuration, and never grant edit authority or support rewrites.
- The installer now reuses ast-grep on `PATH` or downloads an official native release with SHA-256 verification for Linux glibc and macOS on amd64/arm64. Dependency failures stop installation before builds and prompt updates; existing versions are not replaced or upgraded.

### Other

- Added regression coverage for discovery/read/edit authority, output recovery and reducers, live settings, dependency installation failures, and bundled tool workflows.
- Documented structural search, reduction settings, raw-capture limits and third-party attribution; clarified that discovery output and reduced command output do not authorize edits.

## [0.1.2] - 2026-10-16

### Features

- Added `/goal set <objective>`, `/goal check` and `/goal clear`: the agent keeps working until the goal is reached or everything left is blocked, ends it through a `goal_finish` tool, and stops on abort, model error, or three runs without tool calls. `/skill:name` tokens in the objective expand automatically, the goal persists in the session, and the footer shows `GOAL` while it runs.
- Added slash-command namespaces, line-start commands after prose (`//command` to send literally), and separate `/send-steer` and `/send-queue` draft-send commands.
- Added a width-aware two-column TODO widget (72 columns and wider) that keeps closed tasks and follows terminal resizes, and numbered `TODO-X.md` archives in the `todo-planner` skill.
- Added a native Cursor provider that needs no Cursor CLI. It is the default; an explicit `cursor-beta.json` or `PIG_CURSOR_BETA_CONFIG` still selects the CLI beta, and `PIG_CURSOR_TRANSPORT` picks h2 or SSE. Documented in `docs/cursor-direct.md` and `docs/cursor-direct-protocol.md`.
- Added Cursor account meters (Cursor Models, Other Models, on-demand pools) to `/usage`, fetched with each native OAuth account.
- Added `0013-diagnostic-routing-host.patch` and a fused TUI regression (`scripts/test_fused_diagnostics.py`) that keep diagnostics out of the editor draft, cursor, queued input and model history.

### Bugfixes

- Vetoed generation replay and automatic recovery for Cursor and other replay-prohibited providers, added Cursor backpressure handling, and fixed a compaction summary stream drain.
- ASK mode now admits only exact reviewed tool names (`lsp_*`, `code_*`, `memory_*` no longer match by prefix) and denies every shell call.
- Extension diagnostics use `Notify` only, so they no longer enter model history or render over the input.
- Auto-models coalesces repeated credential warnings and keeps the recovery allowance across successful replies and tool continuations.
- Better-footer Git counts disable fsmonitor, skip repositories with clean/process conversion filters, and use a fresh temporary index so stale cached stat data cannot hide edits.

### Other

- Consolidated the patch stack into topic-based patches `0001`-`0013`, removed two unused Node patches, and added `PatchInventoryTest` to check the layout; verified byte-for-byte equivalent source against pinned PiG `0.4.1`.

### Breaking Changes

- `/send` is now `/send-steer` and `/send queue` is now `/send-queue`.
- The direct Cursor provider is the default; configure `cursor-beta.json` or `PIG_CURSOR_BETA_CONFIG` to keep the CLI beta.
- ASK mode rejects every bash call and tools outside the reviewed list; switch to ACT or use the dedicated read and search tools.

## [0.1.1] - 2026-10-15

### Features

- Hashline `edit` now requires the exact-byte SHA-256 `Version` returned by `read`; stale versions are rejected with at most five source-free nearby anchors and a reread instruction.
- Added a session-scoped read-authority engine (64 files / 32 MiB): destructive edits need every affected line fully displayed, and authority is revoked by mutations, session switches, and session start/tree/shutdown events.
- Added a coordinated hashline `write` tool that shares the edit mutation queue, creates private (0600) new files, and replaces existing files explicitly.
- Added cancellation support, bounded mutation admission and request budgets, required directory sync with explicit committed-but-uncertain errors, structured error categories, and payload-free engine metrics.
- Added tool schema bounds, a race-detector CI job (`.github/workflows/hashline-race.yml`), and a tracked golden-fixture generator with a provenance gate (`scripts/hashline-golden`).
- Added `/compact-settings` with persisted relative and absolute automatic compaction limits, capped by PiG's native threshold; `better-footer` shows the effective user-set limit after the context window.
- Added whole-prompt selection in the patched editor: bundled keybindings map Ctrl+A to select all and Home to line start; typing, pasting, and deletion replace the selection, with undo support. Existing keybinding files are not overwritten.
- Bounded stored image previews to 48 hours without use and 256 MiB, with least-recently-used eviction, abandoned temporary-file cleanup, and visible cleanup failures; older sessions can lose evicted previews without losing conversation text.
- Added an opt-in, manually configured, text-only Cursor CLI beta using the selected Cursor OAuth account. Output is buffered, tools and reasoning are unsupported, and live account compatibility remains unverified; setup and limitations are in `docs/cursor-beta.md`.

### Bugfixes

- Stopped edits from silently changing stale interior lines, repeated-line occurrences, or a replaced same-content inode.
- Rejected symlink escapes, hard-linked, read-only, privileged-bit, special-file, and parent-swapped targets; Linux metadata is checked and other platforms fail closed.
- Fixed late-NUL misclassification, unbounded edit summaries, outlines and reads, unknown request fields, unsafe numeric inputs, and temporary-file name collisions.
- Kept preparation files private until final validation and preserved write, close, and cleanup errors.
- Stopped diagnostics from echoing malformed anchors or request values.
- Fixed non-UTF-8 range reads with an over-cap line that returned "lines 1-0" and looped forever; shortened lines are now marked.
- Corrected tool descriptions about staleness validation, range completeness, and empty-file handling.
- Put the command before risk analysis and affected items in Smart Approve confirmation dialogs.
- Made long `/todos` lists scrollable with arrow keys, j/k, page keys, Home, and End, with a visible row-position indicator.
- Added q cancellation alongside Escape to non-text extension menus and patched host select/confirm dialogs, while preserving q as input in search boxes and text editors.
- Preserved registered extension OAuth flows, including Cursor accounts, in the host login menu when the model registry exposes only API-key authentication.
- Submitted SDK compaction requests before event handlers return, preventing the next prompt from racing compaction registration.
- Preserved file, HTTP-response, transport, and temporary-file cleanup failures across configuration, quota, image, LANCET, LSP, update, and search operations; quota/search diagnostics continue to protect credentials.
- Reported malformed LSP workspace-symbol and configuration responses instead of silently ignoring decode errors.

### Other

- Sped up insertion-versus-range conflict checks with a sorted search.
- Documented snapshot expiry, path policy, literal replacement prefixes, and filesystem safety limits.
- Added regression tests for aliases, publication order, commit failures, newline/BOM semantics, range continuation, host boundaries, guard path alignment, and path swaps.
- Slimmed always-loaded agent prompts, consolidated todo guidance for substantial work, and clarified permitted formatter, auto-fixing linter, and LSP workflows without bypassing anchored-edit safety.
- Added regression coverage for the default Smart Approve settings: guard enabled, interactive approval enabled, and LANCET classification disabled.
- Formatted and tightened types in the vendored image-view fixture, locked its dependencies, and added Unicode image-marker boundary and segmentation tests.
- Clarified commit scope naming and the final `BREAKING CHANGE:` footer convention in the format-commit skill.
- Known limits: external compare-to-rename and ancestor-swap races remain, metadata checks are Linux-only, and golden-fixture source provenance is unpinned.

### Breaking Changes

- Hashline `edit` no longer accepts versionless calls or transcript-only anchors. Read the file in the current session, pass its `Version` as `version`, and fully display every line to replace or delete; reread after mutation or authority revocation.
- Hashline mutations now fail closed outside supported Linux filesystem checks and reject unsupported targets or metadata. Use a supported Linux environment and ordinary writable targets; reads remain available on other platforms.
- Repository memory is off by default when no explicit saved setting exists. Run `/pi-curator on` to enable capture, recall, consent prompts, and memory tools; previously saved enabled/disabled settings remain authoritative.

## [0.1.0] - 2026-10-09

### Features

- Initial Go ports of the plugins: LANCET classifier, anchored `hashline-edit` read/edit, `smart-approve-lancet` approval guard, `pi-curator` repository memory, `todo` (phased tasks), and `web-search`.
- Added native read-only `lsp` tools, the `ask-user-question` questionnaire, and `ask-mode` (`/ask`).
- Added `auto-models` with `/usage`, `/auto-model`, scoped model choices, OAuth subscription quota checks, account rotation, and model-slot swapping.
- Added native OAuth accounts, `better-footer` (speed, guard, and memory badges), `cursor-login`, `user-resources`, adhd-output guidance toggle, and image previews.
- Added approval dialogs with risk descriptions, affected paths, "Always allow", and session-model explanations for unknown-scope commands.
- Added the installer (`install.sh`, curl-piped installation, `pig-plugins` link), `dev_build.sh`, piglet manifest, bundled prompts and skills, and compatible-update/search fallback support.

### Bugfixes

- Fixed footer badge and throughput measurement, quota fallback and recovery, and account rotation before cross-provider fallback.
- Fixed Codex context-limit recovery and interrupted websocket retries.
- Persisted interactive approval mode, collapsed tool-output preferences, and default adhd-output mode; stopped reporting expected curator rejections.

### Other

- Added third-party notices and documentation for the ported extensions, installation, and phased todo usage; updated prompts and repository ignore rules.
