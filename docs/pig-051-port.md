# PiG 0.5.1 port inventory

## Status and inputs

Target: pig-plugins 0.2.0 on upstream PiG and extension SDK v0.5.1, tag commit
`1a987d690b62ee3bfb6e4944bf8a8b6968cec170`. The supported manifest is now 0.5.1.
Candidate acceptance passed the full repository suite and fused smoke tests.
The known upstream-only oracle limitation is below.

Compare the official tagged source and SDK, not the installed CLI. Upstream moved
the implementation of `cmd/pig` into `coding/cli`; the executable is now a thin
entry point. CLI patches must follow the implementation rather than add dead code
to `cmd/pig`. Keep host and SDK patch application in separate source trees.

## Patch decisions and acceptance mapping

All patches are retained or adapted pending executable equivalence tests. No
patch is retired merely because a filename or similarly named upstream test exists.

| Patch | Decision / 0.5.1 implementation | Required acceptance |
| --- | --- | --- |
| 0001 native accounts host | Adapt `ai` credential storage, `coding/cli/auth_commands.go`, subprocess dispatch, interactive auth and login UI | Selected-account credentials, concurrent refresh/storage, provider menu registration, account rotation past failed neighbors; no token disclosure |
| 0002 native accounts SDK | Retain account API, check against new context dispatch | SDK account tests plus auto-models/footer/login integration |
| 0003 input and previews | Adapt interactive command routing, autocomplete, editor and transcript hooks | Namespaced/literal/line-start commands, steering/queue, draft recovery, bounded previews, expansion preferences |
| 0004 dialogs | Adapt selector/layout/input handling | Scroll long menus, resize, q/Escape cancellation without consuming q in text input |
| 0005 images host | Adapt subprocess UI bridge and conditional editor | Image numbering, failed neighbors, stale editor replacement, cancellation and retention |
| 0006 images SDK | Retain preview/conditional editor API | SDK payloads and real-host image/draft integration |
| 0007 provider recovery | Adapt overflow/retry code | Codex context-limit classification and interrupted/normal websocket-close behavior |
| 0008 startup versions | Adapt header rendering | Correct host/distribution version, quiet/offline startup and terminal rendering |
| 0009 select all | Adapt editor/keybindings and parity inventory | Selection replacement by type/paste/delete, undo and cursor behavior |
| 0010 ordered compact SDK | Retain ordered dispatch contract | Compaction registered before handler return and next prompt |
| 0011 Cursor host | Adapt message serialization, stream backpressure, session recovery, `coding/cli` RPC events and compaction | Replay veto across all retry paths, exactly-once side effects, tool streaming and interrupted summary drains |
| 0012 Cursor SDK | Adapt provider registration to expanded upstream provider types | Native transport and replay metadata round-trip, provider backpressure/cancellation |
| 0013 diagnostics | Adapt existing upstream diagnostic tests and routing hooks; establish overlap before retiring hunks | Diagnostics never mutate draft/cursor/queued input/model history; fused diagnostic suite |
| 0014 output settings | Adapt settings and slash-handler layout | Persisted opt-in, live toggle, bypass, unchanged exit/error metadata and bounded recoverable output |
| 0015 distribution update | Adapt `coding/cli/self_update.go` and startup initialization, respecting upstream build tags | `update`, `--update`, `--self`, `--force`; extension-only isolation; single binary and safe failed upgrade |

## Bundled extension coverage

Every member of `piglet.yaml` must still register once in the candidate. Existing
unit tests are necessary but not substitutes for fused-host acceptance.

| Members | Integration boundary / acceptance |
| --- | --- |
| check-update | Host version environment, manifest support ceiling, offline preference, no incompatible upstream suggestions |
| tool-search | Discovery and activation without duplicate built-ins or unauthorized edit authority |
| hashline-edit | Version/display/session authority, coordinated write, filesystem guards and image read compatibility |
| command-output | Settings API, exactly-once original Bash execution, unchanged failures, private bounded captures |
| ast-search | argv-safe native ast-grep, missing dependency isolation, bounded discovery-only results |
| smart-approve-lancet | Approval observes original tool/command and cannot bypass hard guards |
| pi-curator | Explicit enable/consent, scoped memory, no capture when disabled |
| lsp | Read-only discovery and bounded diagnostics, no implicit edits |
| web-search | Bounded transport, cancellation and explicit diagnostic failures |
| ask-user-question | Select/text/preview controls, cancellation and terminal sizing |
| todo | Typed prompt composition, ordered phases, bounded resize-aware widget |
| auto-models | Native account selection, quota failure isolation and replay-safe recovery |
| better-footer | Quota/account lookup, context thresholds, badges and failure-safe Git counts |
| pi-image-view | Host preview/conditional editor APIs, numbered references and bounded private storage |
| project-prompt | Typed before_agent_start system prompt and preserved composition |
| adhd-output | Existing prompt preference and persistence through restart |
| repo-excludes | Existing repository exclusion rules at discovery boundaries |
| cursor-login | Native account OAuth, direct provider and explicit CLI-beta selection |
| ask-mode | Typed prompt composition and exact reviewed tool admission; no shell calls |
| goal | Typed prompt composition, session persistence, stop/abort/block semantics |
| compact-settings | SDK compaction ordering and persisted effective thresholds |
| user-resources | Ordered installed/user prompt and skill loading without duplicate resources |

## SDK and user migration review

The upstream v0.5.1 changelog documents the 0.5.0 break from 0.4.x. Confirm definitions
at the target before modifying callers:

- `before_agent_start.systemPrompt` is `*sdk.SystemPrompt`, not a string. Actual
  string consumers exist in goal, todo, ask-mode and project-prompt. Read via
  `Get()` and preserve errors and composition; use host-realistic event fixtures.
- `Context.SetEditorComponent` accepts `sdk.EditorFactory`. Inspect existing calls;
  do not assume every upstream signature change has a repository consumer.
- `agent.NewAgent` returns an error, `Agent.Continue` returns only an error, and
  timing snapshots use `TurnCount`. Review ported host tests and provider code.
- Custom `models.json` models require an explicit `api` unless replacing a built-in.
  Document migration for users; never silently rewrite their configurations.
- Library-only local Packages without resource directories need `"pi": {}` to
  avoid root-extension discovery. Document only applicable package migrations.

## Build and verification gates

1. Resolve the same candidate version for host and SDK independently of pig on
   PATH. Isolate source/workspace stages and bound compiler concurrency.
2. Check each retained patch against pristine sources, then apply in dependency
   order. Set `GIT_CEILING_DIRECTORIES` outside the stage and assert actual source
   changes; a successful Git command that skipped all paths is not evidence.
3. Compile SDK consumers and patched host, then build the real fused distribution.
4. Run focused system tests and race tests, then full repository checks with the
   matching patched SDK. Record known standalone unpatched-SDK failures separately.
5. Run installer fixtures and real-binary update/diagnostic/Cursor/readmap/image
   smoke tests. Use fixture network endpoints, temporary homes and no live secrets.
6. Activate the manifest only after acceptance, rebuild with final pins and verify
   clean install plus previous-layout migration. Compact patches last and compare
   pre/post patched source trees byte-for-byte before rerunning acceptance.

Candidate verification passed with matching patched 0.5.1 host and SDK:

- Full `go test ./... -count=1`, `make fmt-check vet` and `go build ./...`.
- Twenty installer/build fixtures and five fused updater tests, including combined routes and failed-installer cleanup.
- Fused diagnostics, fifty repeated Cursor replay cases, compaction/restart, goal resume and same-session raw-output recovery.
- Fused readmap/security, steering/queue, literal slash and questionnaire ownership tests.
- All four image/TUI protocol variants, selection undo and bounded image payload retention. Focused safety and image race suites passed.

### Upstream test limitations

`TestRPCModelObjectsCarryTheRegistryModelType` fails on eight Radius catalog entries
whose Go objects include `promptCache` fields absent from the pinned JavaScript oracle.
The same eight mismatches occur in a pristine 0.5.1 source copy with identical Node dependencies.
This is not a distribution-patch regression. The test remains unchanged.

Broader upstream Node image tests need vendored chalk files omitted from the Go module archive.
Native image and fused terminal fixtures pass. No live external account or update test was run.

### Boolean launch flags

PiG 0.5.1 treats supplied boolean extension flags as enabled, regardless of their value.
`--adhd false` therefore no longer disables ADHD output. Remove the flag and use `/adhd off`.
That command saves the disabled choice and the default for new sessions.
See [ADHD migration details](adhd-plugin.md).
