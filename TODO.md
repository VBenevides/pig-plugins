# Implementation evidence

## Native OAuth accounts and better-footer

- [x] Implement native PiG OAuth account storage, durable selection, account-scoped login/logout, and selected-account refresh.
- [x] Add account-aware SDK methods and reproducible patches for PiG host and SDK v0.4.1.
- [x] Report each native Codex account independently in `/usage`, including active model and account labels; isolate account failures and caches.
- [x] Implement better-footer generation speed, session usage, project/Git details, recent-model persistence, and exhausted-quota cycling.
- [x] Preserve approval and quota badges without repeating the model name.
- [x] Rebuild `build/pig-plugins` with the patched host and SDK.

Implementation commit: `6a69a181bc9ababbca1e5e7447f6ab2da6a9b4a4`.

### Checks and results

- `make check` with the plain patched PiG host on PATH: passed formatter check, vet, repository tests, and extension validation.
- Race tests for `internal/betterfooter`, `internal/footerstatus`, `extensions/better-footer`, and `extensions/auto-models`: passed.
- Targeted patched-host race tests in `ai` and `internal/codingagent`: passed native account lifecycle, legacy-lock cancellation, refresh metadata/isolation, auth warning, and extension-command behavior checks.
- `./scripts/dev_build.sh`: passed; applied pinned host/SDK patches and published the verified fused executable.
- Bundled executable RPC smoke: `/usage` displayed two synthetic Codex accounts with the correct selected marker and active mock model; no model request or credential leakage. Expired synthetic credentials avoided authenticated network requests.
- Actual TUI startup: replacement footer rendered project/Git fields. Live streamed throughput and real provider account balances were not exercised.
- Native account/security review: no material findings. Earlier footer findings for terminal error sanitization and special-file reads were fixed and regression-tested.

### Limitations and operator steps

- Existing OMP accounts are separate. Log into each account with `/login openai-codex` in the bundled executable, select with `/accounts`, then inspect `/usage`.
- The `openai` direct-API login is not the native `openai-codex` login and does not provide a numeric ChatGPT subscription quota.
- Full upstream host comparison tests need TypeScript comparison dependencies absent from the staged source. Targeted native tests passed; a full host-suite pass is not claimed.
- Windows-specific file handling was not runtime-tested on Linux.
- Rewind remains deferred.
