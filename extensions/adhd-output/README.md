# ADHD Output

A native Go presentation toggle for PiG. The extension adds no model-callable tools, model turns, timers, or background workers.

## Use

Load this extension from the repository:

```sh
pig -e ./extensions/adhd-output
```

| Command | Result |
|---|---|
| `/adhd` | Toggle the active branch's mode |
| `/adhd on` | Enable the rules without a model turn |
| `/adhd off` | Disable the rules and cancel earlier active instructions |
| `/adhd status` | Report the saved mode and current model-context rules |

New sessions default to off. `/adhd on`, `/adhd off` and the `/adhd` toggle also save the chosen mode as `defaultEnabled` in the configuration file (see below), so new sessions start with it. Use `--adhd true` to start one session with the rules enabled. A saved active-branch choice always overrides the default, and an existing session keeps its own choice. If the file cannot be written, the command still applies to the session and shows a warning. A damaged configuration file is never overwritten.

The adapter accepts the SDK's boolean values and PiG's explicit `"true"` / `"false"` CLI strings. Other values produce an error instead of silently changing the default.

The badge `● ADHD ON` uses the host's named status contribution. It does not replace the normal footer. The shared `internal/footerstatus` bridge also makes it available to the fused better-footer renderer.

Print, JSON, and RPC modes do not require a visible footer. The same state and model-context rules apply.

## Session behavior

The extension saves explicit choices as `adhd-output.state` entries with version 1. It reads raw active ancestry through `SessionManager.GetBranch`. It does not select the final state from all session entries.

Session start, resume, branch selection, and a new extension instance reconstruct the active choice from session records. Compaction does not remove that choice.

The extension sends the embedded `rules.md` as an invisible custom message. Its `adhd-output.rules.v1` marker includes a SHA-256 content version. An explicit `TriggerTurn: false` prevents a model completion.

Repeated enable commands do not append another active copy. A successful `session_compact` event reads `BuildSessionContext`, the host's compaction-aware model messages. It restores the rules only if the current active copy is absent. Failed or canceled compaction does not trigger synchronization.

Disable clears only the extension's badge. If active historical rules remain, the extension sends an invisible `adhd-output.disabled.v1` cancellation notice. Historical rules stay in session history until compaction removes them.

The rules affect presentation only. The extension does not replace the system prompt, tool descriptions, project instructions, or other extension contributions. Do not permanently duplicate the ADHD rules in `APPEND_SYSTEM.md` or another prompt extension. Such a copy cannot be disabled by `/adhd off`.

## Optional configuration

Only `/adhd on`, `/adhd off` and the toggle create this extension-owned file (to save `defaultEnabled`). You can also edit it by hand:

```text
~/.pig/state/adhd-output/config.json
```

When `PIG_HOME` is set, use `$PIG_HOME/state/adhd-output/config.json` instead.

```json
{
  "defaultEnabled": false,
  "showStatus": true,
  "rulesFile": "rules.md"
}
```

All fields are optional. Omit `rulesFile` to use the bundled exact presentation rules. A relative `rulesFile` path uses the configuration directory. The configuration limit is 64 KiB. The rules limit is 128 KiB. Empty or unreadable override files produce an error, not a silent fallback.

`showStatus: false` hides only the badge. Configuration reloads on session start or branch selection. `/reload` creates a new extension instance and reads the configuration again.

## Host contracts and limits

This repository pins its local SDK staging to PiG SDK **v0.4.1** in `scripts/gowork.sh`. Use the same PiG release and the repository's host patches. This package uses the root Go module and Go 1.26. It does not need a separate module or Node.js.

The factory is `func Extension() *sdk.Extension`. It is eligible for fused realization. Add this extension to the parent Piglet selection and set `build.extensionRealization: fused` there. This extension alone does not make unrelated Node extensions native.

Callbacks use a bounded serial coordinator. No coordinator mutex stays locked during host IPC. Session identity and leaf checks reject stale work before writes. The SDK has no atomic compare-and-append API. A host branch switch inside a mutation cannot be prevented by an extension. The next lifecycle event reconstructs the actual selected branch.

While a model turn streams, PiG can queue explicit-false custom messages until its tool results finish. The extension tracks the latest queued style change, suppresses duplicates, and checks it on turn end and agent settlement. It hides the badge until the rules actually appear in model context.

The SDK does not expose queued custom-message contents. A reload during an unpersisted streaming delivery cannot inspect that queue. After the host persists the message, reload is idempotent. Use toggle commands while idle if a reload must occur immediately.

PiG can report a delivery error asynchronously while its `SendMessage` call returns success. The extension checks the model context before it shows a badge. An idle missing message produces an error. A queued message that stays absent after settlement also produces an error.

## Tests

From the repository root, use the version-matched workspace:

```sh
go test -race ./internal/adhdoutput ./extensions/adhd-output
go test ./internal/adhdoutput -run '^$' -bench BenchmarkLifecycle -benchmem
```

The extension tests use the real `pig` executable and the repository's HTTP mock provider. They skip provider-bound checks when `pig` is not on `PATH`. Put the patched binary on `PATH` for the integration checks.

Tests cover active-branch state, resume with a new extension instance, new sessions, idempotence, rapid toggles, concurrent compaction synchronization, failure visibility, and queued streaming delivery. They also capture actual provider requests to check base prompt and tool preservation, cancellation, and restored rules after persisted compaction.

`testdata/session-probe` supplies test-only session transitions and a controlled compaction summary. It adds no model tool. Successful and canceled compaction tests run the real host persistence and event paths without a summarizer model call.

The failed-compaction test uses a scripted HTTP provider error. PiG reports errors from `session_before_compact` handlers but continues with default summarization, so a handler error alone does not test failed compaction.

`TestPerformanceComparison` compares absent, disabled, and enabled variants. It logs startup time, idle process-tree RSS/PSS on Linux, and handler p50/p95 latency. The absent variant uses a `get_state` round trip as its baseline. Memory limitations are reported, not treated as zero overhead.

The provider-context test logs exact rules bytes and a character/4 token estimate. A mock provider cannot measure a real provider's tokenizer. Report this estimate as an estimate, not as billed token usage.
