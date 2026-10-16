# Cursor models beta

This page describes the legacy CLI adapter, not the default [CLI-free direct provider](cursor-direct.md).
An explicit beta configuration keeps this adapter selected; no automatic migration or fallback occurs.

This is an **opt-in, text-only beta**, not a verified Cursor model API integration.
No Cursor account or live inference was available during development. Account
entitlements, credential compatibility and current CLI behavior still require a
live acceptance check. Existing `/login` Cursor accounts are preserved.

## Prepare the CLI

Install the official [Cursor CLI](https://cursor.com/docs/cli/overview.md) yourself
on the target machine. This extension does not download or update executables.
Use a trusted absolute executable path. Check its version and list models:

```sh
/absolute/path/to/cursor-agent --version
/absolute/path/to/cursor-agent models
```

The second command needs an account in that CLI. Do not paste credentials into
chat, configuration examples or command arguments. Alternatively, obtain the
exact model identifier and limits from your account's current documentation.
CLI login storage is **not** copied into PiG; use `/login` → Cursor account in
PiG for inference. The transport passes the selected account's access token via
`CURSOR_AUTH_TOKEN` to an isolated CLI process. This path is documented for ACP,
but compatibility with print mode has **not been verified live**. If it does not
work, disable the beta and report the failure; do not expose your token.

## Enable models

Create `~/.pig/agent/cursor-beta.json`, or set `PIG_CURSOR_BETA_CONFIG` to a
configuration file. `PIG_CODING_AGENT_DIR` changes the default agent directory.
The file contains no credentials. Replace the path, ID, name and token limits:

```json
{
  "cli": "/absolute/path/to/cursor-agent",
  "models": [
    {
      "id": "REPLACE_WITH_ACCOUNT_MODEL_ID",
      "name": "My Cursor model",
      "contextWindow": 8192,
      "maxTokens": 1024
    }
  ]
}
```

The numbers above are **example local metadata**, not asserted Cursor limits.
Set limits appropriate to the selected model. The CLI controls generation and
may apply its own output limits; this adapter cannot enforce an exact token
budget. Model entries are manually configured, not discovered or entitlement
checked. When this file is absent, the default direct provider performs account-scoped discovery.
Invalid configuration produces a session-start error and leaves account login available without
silently selecting a different transport. Restart after changing the file.

Rebuild with `./scripts/dev_build.sh`, then run:

```sh
./build/pig-plugins --no-tools --thinking off --model cursor/REPLACE_WITH_ACCOUNT_MODEL_ID
```

Log into Cursor through `/login` first. Configured names include “Cursor beta,
text only”. Move the configuration file aside and unset its environment override to use the direct provider.

## Safety and limitations

- Text requests only: no PiG tool schemas/results, images or reasoning. Requests
  containing these features fail rather than silently lose them. Cursor receives
  a textual rendering of system instructions and conversation roles, not native
  role-separated model messages. Its own system instructions can still apply.
- Each request uses a private, disposable home/workspace, read-only `ask` mode,
  and explicit deny rules for shell, file reads/writes, web fetch and MCP. It does
  not inherit project settings, MCP servers, hooks, ambient account credentials,
  custom endpoints, or proxy variables. It never uses `--force` or `--trust`.
  These are CLI-level restrictions, **not an OS security sandbox**; use only a
  trusted CLI. Actual enforcement must be verified against your CLI version.
- Unexpected CLI tool events are errors, not PiG tool requests. This check cannot
  undo an operation performed by a faulty or incompatible CLI. PiG-controlled
  MCP tool proposals are implemented separately in the direct provider, not in
  this CLI beta; Cursor-native exec remains unsupported in the direct provider.
- Output is buffered until the CLI reports success and exits successfully.
  There is no incremental display, automatic retry, account fallback, or remote
  session resume. Partial/truncated/malformed output never becomes success.
- A request has a two-minute deadline; prompts and output are capped at 4 MiB,
  configuration at 64 KiB and model entries at 100. Cancellation terminates the
  direct CLI process; this beta does not guarantee descendant process containment.
- No token usage or prices are fabricated. The documented CLI result has no
  token-usage contract; missing/zero host statistics mean **unknown**, not free
  usage. Cursor requests may consume quota or incur charges.
- CLI stderr and error payloads are withheld because they can contain secrets or
  prompt data. Failures retain operation/exit or protocol context. Do not enable
  verbose credential logging to troubleshoot.
- Non-default sampling/reasoning parameters are rejected. Native role fidelity,
  dynamic model discovery, streaming deltas, accurate usage and complete account
  isolation/refresh acceptance are not production-ready features of this beta.

## Account-free verification

```sh
GOTOOLCHAIN=go1.27.2 go test ./internal/cursorcli ./extensions/cursor-login
GOTOOLCHAIN=go1.27.2 go test -race ./internal/cursorcli ./extensions/cursor-login
GOTOOLCHAIN=go1.27.2 go vet ./internal/cursorcli ./extensions/cursor-login
```

Tests use fake CLI processes. They cover opt-in registration, selected-token
transport, isolated configuration, denied-tool configuration, text conversion,
protocol/exit failures, size bounds and cancellation. They do not establish
service access or verify Cursor's own enforcement. On an account-enabled target,
first verify `/login`, model selection and a short text response; separately
verify cancellation, expired-token refresh and that tool access is denied before
using sensitive context. Never represent these manual checks as already passed.

## Protocol sources

- [Cursor API overview](https://cursor.com/docs/api.md): explicitly distinguishes
  agent workflows from standalone inference/chat-completions APIs.
- [CLI parameters](https://cursor.com/docs/cli/reference/parameters.md): model
  selection, print mode, and account model listing.
- [CLI output format](https://cursor.com/docs/cli/reference/output-format.md):
  NDJSON events and terminal success contract.
- [ACP authentication](https://cursor.com/docs/cli/acp.md): `CURSOR_AUTH_TOKEN`.
- [CLI permissions](https://cursor.com/docs/cli/reference/permissions.md): deny
  rules take precedence over allow rules.
- [SDK bridge](https://cursor.com/docs/sdk/bridge.md): supported agent SDK surface
  with separate API-key and loopback-bridge credentials, not a drop-in model API.

Public docs and CLI `2026.10.14-941492e --help` were inspected. No authenticated
model listing or generation was performed. Recheck these version-sensitive
contracts before production use.
