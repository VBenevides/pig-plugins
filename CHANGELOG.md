# Changelog

All notable changes to pig-plugins are documented here.

## [0.1.1] - 2026-10-12

### Features

- Hashline `edit` now requires the exact-byte SHA-256 `Version` returned by `read`; stale versions are rejected with at most five source-free nearby anchors and a reread instruction.
- Added a session-scoped read-authority engine (64 files / 32 MiB): destructive edits need every affected line fully displayed, and authority is revoked by mutations, session switches, and session start/tree/shutdown events.
- Added a coordinated hashline `write` tool that shares the edit mutation queue, creates private (0600) new files, and replaces existing files explicitly.
- Added cancellation support, bounded mutation admission and request budgets, required directory sync with explicit committed-but-uncertain errors, structured error categories, and payload-free engine metrics.
- Added tool schema bounds, a race-detector CI job (`.github/workflows/hashline-race.yml`), and a tracked golden-fixture generator with a provenance gate (`scripts/hashline-golden`).

### Bugfixes

- Stopped edits from silently changing stale interior lines, repeated-line occurrences, or a replaced same-content inode.
- Rejected symlink escapes, hard-linked, read-only, privileged-bit, special-file, and parent-swapped targets; Linux metadata is checked and other platforms fail closed.
- Fixed late-NUL misclassification, unbounded edit summaries, outlines and reads, unknown request fields, unsafe numeric inputs, and temporary-file name collisions.
- Kept preparation files private until final validation and preserved write, close, and cleanup errors.
- Stopped diagnostics from echoing malformed anchors or request values.
- Fixed non-UTF-8 range reads with an over-cap line that returned "lines 1-0" and looped forever; shortened lines are now marked.
- Corrected tool descriptions about staleness validation, range completeness, and empty-file handling.

### Other

- Sped up insertion-versus-range conflict checks with a sorted search.
- Documented snapshot expiry, path policy, literal replacement prefixes, and filesystem safety limits.
- Added regression tests for aliases, publication order, commit failures, newline/BOM semantics, range continuation, host boundaries, guard path alignment, and path swaps.
- Known limits: external compare-to-rename and ancestor-swap races remain, metadata checks are Linux-only, and golden-fixture source provenance is unpinned.

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
