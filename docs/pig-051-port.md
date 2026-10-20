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

The accepted fifteen-patch stack is consolidated into nine cohesive patches.
Host and SDK remain separate. Apply each target's files in numeric order.
The original and compacted stacks produce byte-identical host and SDK trees.

| Patch | Original patches | System and acceptance |
| --- | --- | --- |
| `0001-accounts-host.patch` | 0001 | Native credential storage, account rotation, login UI and secret-safe errors |
| `0002-accounts-sdk.patch` | 0002 | Typed account API and dispatch |
| `0003-editor-images-host.patch` | 0003, 0004, 0005, 0009 | Input namespaces, previews, dialogs, images, selection and undo |
| `0004-editor-images-sdk.patch` | 0006 | Image preview and conditional-editor SDK |
| `0005-provider-recovery-host.patch` | 0007, 0011 | Overflow/replay vetoes, Cursor transport, startup discovery and backpressure |
| `0006-provider-recovery-sdk.patch` | 0012 | Provider registration, replay metadata and cancellation |
| `0007-distribution-host.patch` | 0008, 0015 | Startup versions, single executable and distribution-owned updates |
| `0008-settings-diagnostics-host.patch` | 0013, 0014 | Clean diagnostic routing, live settings, persistence and raw recovery |
| `0009-compaction-sdk.patch` | 0010 | Ordered compaction before subsequent prompts |

Consolidation checks passed: independent pristine-tree application and byte-for-byte
comparison, three inventory checks, a fresh fused build, full repository tests with
bundled smoke fixtures, `make fmt-check vet`, `go build ./...`, 21 installer
fixtures, five updater fixtures, and fused TUI diagnostics. `scripts/gowork.sh`
also rebuilt an SDK identical to the independently applied stack.

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
