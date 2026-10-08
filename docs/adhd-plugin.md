# ADHD Output — Native Go Extension for PiG

**Status:** Implementation specification (not implemented)

**Target:** PiG (Pi in Go), using its public Go extension SDK

**Extension ID:** `adhd-output`

**Purpose:** Provide a lightweight, toggleable ADHD-friendly response style that survives session resume, branch changes, extension reloads, and context compaction, with an unobtrusive indicator in PiG's existing footer.

## 1. Goals and non-goals

### Goals

1. Implement the extension in **Go**, without Node.js or TypeScript.
2. Toggle with `/adhd`, `/adhd on`, `/adhd off`, and inspect with `/adhd status`.
3. Make the style rules available to the **model**, not merely change how the TUI displays text.
4. Make mode changes take effect on the **next model request** and restore the active mode after context compaction.
5. Persist the effective mode **per session / active branch**, not globally by accident.
6. Show `● ADHD ON` in the existing footer/status area when enabled; remove this indicator when disabled.
7. Coexist with other footer extensions and leave the user's existing `SYSTEM.md`, `APPEND_SYSTEM.md`, tools, and safety policies untouched.
8. Support execution as a normal Go extension and, later, fusion into a native PiG/Piglet binary.
9. Avoid unnecessary model calls, continuous polling, background goroutines, repeated injections, and additional model tools.

### Non-goals

- Diagnosing or treating ADHD.
- Changing the reasoning/model provider, editing tools, safety policy, or coding behavior.
- Replacing the entire footer or system prompt.
- Implementing a new TUI component, renderer, or background service.
- Automatically editing the user's `APPEND_SYSTEM.md`.

## 2. User experience

| Action | Expected result |
|---|---|
| `/adhd` | Toggle the current session's mode |
| `/adhd on` | Enable ADHD-friendly output; no-op if already enabled |
| `/adhd off` | Disable mode; no-op if already disabled |
| `/adhd status` | Report enabled/disabled state and whether the rules are currently in model context |
| Start a new session | Use the configured default (off unless overridden) |
| Resume a session | Restore that session's last active-branch choice |
| Switch to a different branch | Restore the choice on the selected branch |
| Compact the context | Reintroduce rules **only if enabled and no active copy survives** |
| `/reload` | Restore state; no duplicate rule injection |

Optional launch flag: `pig --adhd` starts a new session with the mode enabled. A saved choice on the active branch **always wins** over launch/config defaults.

### Footer behavior

When enabled, show:

```text
● ADHD ON
```

When disabled, the extension's status entry should be absent, not replaced by `ADHD OFF`.

Use PiG's **status contribution** mechanism, **not** `SetFooter`, which replaces the normal footer. The status entry must coexist with models, quota, context, and TPS indicators. Color is optional and must not be required for understanding.

## 3. Architecture

```text
                  PiG (native Go)
                        │
                  adhd-output
                        │
         ┌──────────────┼────────────────┐
         │              │                │
    /adhd command   session state    output rules
         │              │                │
         └──────┬───────┘                │
                ▼                        ▼
      current enabled state     model-visible context
                │                (idempotent)
                ▼                        ▲
        ctx.SetStatus(...)         session_compact
                │
                ▼
          normal PiG footer
```

The plugin should have **zero model-callable tools**: this is a UI/session behavior extension only.

## 4. Source layout

```text
adhd-output/
├── go.mod
├── extension.go            # func Extension() *sdk.Extension
├── state.go                # session/branch state and persistence
├── prompt.go               # rule injection and idempotency
├── status.go               # indicator and notifications
├── config.go               # defaults, optional launch flag
├── rules.md                # user-editable style instructions
├── extension_test.go
├── state_test.go
├── prompt_test.go
└── README.md
```

Prefer a factory-style Go extension so it remains eligible for PiG's fused extension realization. Do not add an executable `package main` to the same source package as the `Extension()` factory.

Keep dependencies minimal: PiG SDK and Go standard library. Use `go:embed` to load the default rules, if appropriate, while allowing an explicit override from a documented extension-owned configuration path.

## 5. Go SDK contracts

At the time of design, the public PiG SDK includes these relevant APIs:

```go
import "github.com/MichaelKinsy/PiG/extensions/sdk"

func Extension() *sdk.Extension

sdk.New("adhd-output")

ext.Command(name, description string, handler sdk.CommandFunc)
ext.Flag(name string, options sdk.FlagOptions)
ext.OnSessionStart(handler sdk.EventFunc)
ext.OnEvent(eventName string, handler sdk.EventFunc)

ctx.SetStatus(key, text string)
ctx.Notify(message, level string)
ctx.AppendEntry(customType string, data any) error
ctx.GetEntries() ([]json.RawMessage, error)
ctx.GetBranch() ([]sdk.BranchEntry, error)
ctx.SendMessage(customType, content string, display bool, opts sdk.SendMessageOptions) error
ctx.GetFlag(name string) (any, error)
```

`SetStatus` uses `""` to clear the named status entry. Use a private, stable key such as `"adhd-output"`. Never call `SetFooter` just to add the badge.

These signatures are based on PiG's public Go SDK. **Pin a PiG SDK release and compile against that exact version** rather than assuming `main` will remain API-stable. Not every TypeScript Pi extension behavior maps to direct mutation of Go event payloads.

### Skeleton (illustrative, not a complete plugin)

```go
package adhdoutput

import (
    "strings"

    "github.com/MichaelKinsy/PiG/extensions/sdk"
)

const statusKey = "adhd-output"

func Extension() *sdk.Extension {
    ext := sdk.New("adhd-output")

    ext.Flag("adhd", sdk.FlagOptions{
        Description: "Start with ADHD-friendly output enabled",
        Type:        sdk.FlagBoolean,
        Default:     false,
    })

    ext.Command("adhd", "Toggle ADHD-friendly output", func(ctx sdk.Context, args string) error {
        switch strings.ToLower(strings.TrimSpace(args)) {
        case "", "on", "off", "status":
            // Dispatch to session-aware, persisted implementation.
            // See Sections 6–9 below.
            return handleADHDCommand(ctx, args)
        default:
            ctx.Notify("Usage: /adhd [on|off|status]", "warning")
            return nil
        }
    })

    ext.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
        return nil, restoreAndSync(ctx)
    })
    ext.OnEvent("session_tree", func(ctx sdk.Context, _ map[string]any) (any, error) {
        return nil, restoreAndSync(ctx)
    })
    ext.OnEvent("session_compact", func(ctx sdk.Context, _ map[string]any) (any, error) {
        return nil, syncRulesWithCurrentContext(ctx)
    })

    return ext
}
```

`handleADHDCommand`, `restoreAndSync`, and `syncRulesWithCurrentContext` are intentionally **not defined in this example**; implement and test them using the state and context rules below. This snippet illustrates registration, not an already working release.

## 6. State model and defaults

Use two separate concepts:

- **Configured default:** `off` by default; may be overridden by `--adhd` or optional config.
- **Effective session state:** the most recent explicit on/off choice on the **active session branch**.

Precedence:

```text
latest active-branch explicit choice
                >
CLI launch flag / configured default
                >
off
```

Suggested custom session entry:

```json
{
  "customType": "adhd-output.state",
  "data": {"version": 1, "enabled": true}
}
```

Persist explicit toggles using `ctx.AppendEntry("adhd-output.state", ...)`. Do not store every toggle exclusively in a global JSON file: changing sessions must not leak the previous session's preference.

**Important SDK detail:** the Go SDK's convenient `GetBranch()` projection does not expose every custom entry's `data` field. The implementation must use an appropriate branch-aware/raw session entry API, verify active-branch ancestry, and decode the custom entry data safely. `GetEntries()` by itself includes entries that may belong to other branches, so blindly selecting its final custom state entry would be incorrect.

On failed retrieval or malformed state, do **not** silently pretend the session was explicitly disabled. Report a useful warning and avoid destructive state changes. On session change or extension reload, always re-read session state; do not trust process-local booleans.

Optional extension-owned defaults may live under:

```text
~/.pig/state/adhd-output/config.json
```

Example:

```json
{
  "defaultEnabled": false,
  "showStatus": true
}
```

Only create this file when the user needs a nondefault configuration. Normal sessions should not require any file writes beyond PiG's own session records.

## 7. Prompt/context injection strategy

**Primary implementation:** follow the established Pi `i-have-adhd` pattern of an invisible custom message holding the style rules, with marker-based idempotence and re-injection after compaction.

Reason: PiG's Go SDK provides `SendMessage`, session events, and custom entries. Directly modifying a TypeScript-style `event.systemPromptOptions.sections` map **across Go's subprocess boundary is not something to assume without a conformance test**. Avoid returning an opaque replacement `systemPrompt`: that risks discarding other extensions' prompt sections.

### Active-context markers

Define two distinct custom message types:

```text
adhd-output.rules.v1
adhd-output.disabled.v1
```

On enable:

- If the newest relevant model-visible marker indicates the current rules version is active, do nothing.
- Otherwise send **one** invisible custom message containing the full `rules.md` text plus a version marker.
- Use `sdk.SendMessageOptions{TriggerTurn: sdk.Bool(false)}` or the equivalent pinned-SDK explicit false option: injecting rules must **not start an LLM turn**.
- Show the footer badge only after the mode is effectively enabled; report injection failures instead of claiming success.

On disable:

- Persist `enabled=false`.
- Clear the footer badge.
- If an enabled rules message remains in the active model context, send a short invisible cancellation notice indicating the earlier style instructions are no longer in effect.
- Never restart the agent or trigger a model completion just for the toggle.

### On compaction

Subscribe to `session_compact` (successful persisted compaction), not only `session_before_compact`.

If enabled:

1. Inspect the **current post-compaction model-visible context**.
2. If an active marker/rules body survived, do nothing.
3. Otherwise inject the rules once, without triggering a turn.

If disabled:

1. Do not re-inject the rules.
2. If an old active marker somehow survives, ensure the next model turn receives the cancellation notice.

Compaction failure or cancellation must **not** be treated as a successful compact, and must not cause duplicate injection.

### Why not inject on every turn?

Constant insertion grows the context unnecessarily. The desired behavior is **once when enabling + once after each compaction that removes it**, not once for every request. On resume/branch switch, inspect the active context and inject only if absent.

### Future optimization: structured prompt section

A second implementation may use a named per-turn system-prompt section, `adhd_output`, **only after testing PiG's Go SDK event-return semantics**. It would have these benefits:

- rules present while on, absent while off;
- no old style message retained after disabling;
- automatic regeneration after compaction;
- independent prompt-section updates.

Acceptance gate: prove enabling/disabling affects the actual provider-bound system message without replacing the base prompt or any other extension's sections. Until then, ship the invisible-message implementation above. Do not simply mutate a locally decoded Go map and assume PiG will observe it.

## 8. Exact ADHD output rules

Store the following in `rules.md`. These rules affect **response presentation** and must not weaken tool safety or coding instructions.

```markdown
# ADHD-Friendly Output

Apply these presentation rules when ADHD output mode is enabled.

1. **Result or action first.** Start with the answer, outcome, or immediate useful action; skip routine preambles.
2. **Number real procedures.** For multi-step work, give a short ordered sequence with clear commands or paths.
3. **Limit cognitive load.** Prefer short cohesive paragraphs, focused sections, and at most five list items unless more are genuinely necessary or requested.
4. **Suppress tangents.** Separate optional background from what is necessary to proceed. Avoid needless alternatives and duplicated cautions.
5. **Preserve task state.** In longer workflows, briefly say what is complete, what remains, and where the user should resume after interruption; don't repeat full recaps every turn.
6. **Make milestones visible.** State meaningful completion and blockers matter-of-factly; do not over-celebrate routine steps.
7. **Be specific.** Use exact file paths, commands, expected results, and actionable diagnostics. Give time estimates only when justified.
8. **Show failures directly.** Say what failed, what is known, and the next relevant diagnostic; never imply verification was performed if it was not.
9. **Recommend one default.** When choices exist, explain the preferred option and one main tradeoff before expanding.
10. **End naturally.** End with one next action only when useful; otherwise stop after the result. No boilerplate closing lines.

These rules control presentation only. They never override higher-priority safety requirements, project instructions, or the tool contracts in the active system prompt.
```

Do not duplicate these rules in `APPEND_SYSTEM.md` when using this toggleable extension; otherwise `/adhd off` would not actually disable them.

## 9. Footer/status integration

Use the existing PiG status API:

```go
func updateStatus(ctx sdk.Context, enabled, visible bool) {
    if !enabled || !visible {
        ctx.SetStatus("adhd-output", "")
        return
    }
    ctx.SetStatus("adhd-output", "● ADHD ON")
}
```

Requirements:

- Status text is a dedicated named contribution, not a custom footer replacement.
- Reapply after session start, branch switch, resumed session, or reload.
- Clear immediately on `/adhd off`, if supported by the active UI.
- If there is no interactive TUI (print/JSON/RPC mode), state and prompt injection must still work; lack of a visible footer must not fail the extension.
- Do not use status updates to emit new model messages.
- Optionally honor `showStatus=false` to hide the badge without disabling the rules.

The PiG Go SDK exposes `Context.SetStatus(key, text)`. Empty text clears that key.

## 10. Lifecycle and concurrency

| Event | Required work |
|---|---|
| `session_start` | Load effective state; update footer; verify/inject rule marker |
| `session_tree` | Recompute active branch state; update footer; verify/inject marker |
| `session_compact` | Keep mode; re-inject only if missing from post-compact context |
| `/reload` (new extension instance) | Reconstruct state from session; do not use stale in-memory flags |
| `/adhd on/off` | Persist decision, sync context, update footer, notify briefly |
| `session_shutdown` | Clear transient resources, if any; no background work should be running |

The SDK can deliver callbacks in independent goroutines. Protect shared in-memory state with `sync.Mutex` or a narrowly scoped serial state coordinator. Avoid holding that lock across host IPC calls (`AppendEntry`, `SendMessage`, `SetStatus`) if those calls may reenter callbacks. Use a generation/session-ID check to reject late work from a previous session or branch. Serialize toggle and compact synchronization so concurrent events cannot produce duplicate injections.

Do not start timers or background poll loops; this plugin has no periodic work.

## 11. Installation and packaging

### Scaffold

```bash
pig extension init ./adhd-output --lang go
```

Adapt the scaffold to export exactly:

```go
func Extension() *sdk.Extension
```

Use the Go SDK module required by the **same PiG release**. Do not mix SDK versions.

### Validate

```bash
pig install ./adhd-output --validate-only --json
pig -e ./adhd-output
```

Within the interactive PiG session:

```text
/adhd status
/adhd on
/adhd off
/adhd on
```

The first command must report state; the following commands must change the badge and model-visible rules without initiating model calls.

### Optional fused Piglet

PiG documents factory-style Go extensions as eligible for fusion into a Piglet Binary. After the source extension passes tests, select it in a Piglet and require:

```yaml
build:
  extensionRealization: fused
```

This must build a binary without a Node extension host for this extension. An all-fused composition will fail if any selected extension cannot be fused; do not claim the entire agent is Node-free when other extensions still require Node.

## 12. Tests and acceptance criteria

### State/lifecycle tests

- Default off → no status badge, no rules injected.
- `/adhd on` → one rules message and `● ADHD ON`.
- Repeated `/adhd on` → **no extra rules message**.
- `/adhd off` → no badge, cancellation of prior active rules, no model request.
- `/adhd on` after off → active rules exactly once.
- New session respects configured default, not the previous session's toggle.
- Resume retains last explicit active-branch choice.
- Branch to an earlier off state restores off; returning to the on branch restores on.
- Reload reconstructs state without duplicate injections.
- Compaction with removed rules injects once; compaction with retained rules injects zero times.
- Failed/aborted compaction injects nothing solely because of the failure.
- Rapid on/off and concurrent compaction do not race or duplicate marker messages.

### Footer tests

- Status is visible in the **normal footer** and coexists with model, context, TPS, or other status entries.
- Disabling removes only the `adhd-output` status key.
- Narrow terminal widths and non-interactive mode do not crash.

### Prompt integrity tests

- Existing `SYSTEM.md`, `APPEND_SYSTEM.md`, project instructions, tool descriptions, and unrelated extension contributions remain unchanged.
- Rules are present in the provider-bound context on the first turn after enable and after a compact that removed them.
- Rules do not appear in newly built model contexts while disabled, except unavoidable historical text accompanied by the explicit disabled marker (in the message-based implementation).
- There are **no model calls** caused by `/adhd on/off/status` alone.
- Extra prompt tokens are measured, and no unbounded duplicate growth occurs between compactions.

### Performance tests

Compare plugin disabled, enabled, and absent for:

```text
startup overhead
idle process RSS/PSS
handler latency (p50/p95)
additional prompt tokens
compaction restoration correctness
```

For a lightweight status/rules extension, prioritize behavioral correctness and zero unnecessary model calls over micro-optimizing Go execution.

## 13. Definition of done

- [ ] Go factory extension compiles against a pinned PiG SDK release.
- [ ] Commands toggle and report session state correctly.
- [ ] Footer displays `● ADHD ON` while enabled, coexisting with other footer content.
- [ ] Current active branch's preference survives resume, fork, switching, and reload.
- [ ] Rules are injected exactly when necessary; no duplicates between compactions.
- [ ] Post-compaction restore is correct and does not trigger a new agent turn.
- [ ] Disabling removes badge and deactivates rules for future model turns.
- [ ] No modification of `SYSTEM.md` or permanent `APPEND_SYSTEM.md` is required.
- [ ] No Node.js dependency is introduced by this plugin.
- [ ] Tests pass in normal and (if used) fused execution.

## 14. References verified during design

- PiG extension architecture and Go factory/fusion: <https://pi-in-go.dev/docs/latest/extensions/>
- PiG Go SDK extension registration (`Extension`, `Command`, `Flag`, `OnEvent`): <https://raw.githubusercontent.com/MichaelKinsy/PiG/main/extensions/sdk/extension.go>
- PiG Go SDK context (`SetStatus`, `AppendEntry`, `SendMessage`, `GetEntries`): <https://raw.githubusercontent.com/MichaelKinsy/PiG/main/extensions/sdk/context.go>
- Original Pi `i-have-adhd` extension (session marker, compaction, status): <https://github.com/ayghri/i-have-adhd/blob/main/extensions/i-have-adhd.ts>
- PiG Piglet binaries: <https://pi-in-go.dev/docs/latest/piglet-binaries/>

**Implementation note:** This document specifies the implementation and includes illustrative Go registration examples; it does not assert that the completed plugin has been built or tested.
