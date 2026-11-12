# Tool exposure

## Policy

Permission and exposure are separate states. A permission allows execution through normal hooks. Exposure determines which declarations reach the model.

| Mode | Initial declaration | Discovery | Execution |
|---|---|---|---|
| Direct | Included when active | Not needed | Normal hooks |
| Deferred | Omitted until activated | `tool_search` or codemode | Normal hooks |
| Conditional | Included only while its condition is true | Must not bypass the condition | Check the condition again |
| Backend | Omitted from direct use | Prefer wrapper-only access | Normal hooks through the wrapper |

The approved direct core is `read`, `edit`, `write`, `bash`, and `tool_search`. Audit exceptions before rollout.
Discovered tools should remain active through the current user request. Release them at a safe boundary.
If discovery is unavailable, preserve compatible behavior and show a warning. Do not expose the entire catalog silently.

## Source inventory

This inventory describes the pinned PiG 0.5.1 SDK and local registrations. It does not claim that all installed MCP servers are known.

| Owner | Tools | Registered exposure | Condition or dependency |
|---|---|---|---|
| Host | `read`, `edit`, `write`, `bash` | Direct | Permissions and file implementation |
| Host and tool-search extension | `tool_search` | Model-only | Built-in discovery must be installed and active |
| Goal extension | `goal_finish` | Direct with a loadout guard | Working goal and permission |
| Web-search extension | `web_search` | Deferred | Discovery, or existing direct fallback without discovery |
| Web-search extension | `url_context` | Direct | Compatible conversation model |
| Web-search MCP registration | Parallel and Exa tools | Deferred MCP | Wrapper fallback and permissions |
| LSP extension | `lsp_*` | Deferred | Language server availability |
| AST search extension | `ast_search` | Deferred | Parser support |
| Pi curator extension | Memory tools | Deferred | Memory service configuration |
| Browser extension | Browser tools | Registration-dependent | Browser configuration and activation |
| Todo extension | `todo` | Deferred | Direct fallback with a warning when discovery is inactive |
| Ask-user-question extension | `ask_user_question` | Model-only | Interactive input support |
| Host or other providers | Repository and recovery tools | Inspect runtime metadata | Do not infer ownership or dependencies from tool names |

`ToolInfo.Exposure` reports registration metadata. `GetActiveTools` reports activation, not permission.
`HiddenTools` alone does not establish the exact provider request or the reason a declaration is absent.
Runtime MCP inventories can change. Use actual registry metadata and captured requests for unknown tools.

## Verified host behavior

The pinned host implements discovery in `coding/extension/builtin/toolsearch/tool.go`.
It searches only `deferred` and `codemode` registrations that are not active.
It activates matches through `SetActiveTools`. Results contain names and descriptions, not complete schemas.
Direct tools such as `goal_finish` are not discovery candidates.
The native search loop does not consult this project's saved permission file or a general lifecycle predicate.
The tool-search extension filters denied activations before the next declaration. It redirects verified search dependencies to web_search.
It releases only its reported discoveries at agent_settled. Pre-existing and codemode-owned activations are not removed.
Search accepts at most 4096 query bytes and 20 results. A request can activate at most 20 tools through this extension.
The host can rank denied candidates before filtering. A search can therefore return fewer allowed matches than requested.

The SDK provides `PrepareLoadout` and `HiddenDeclarations`. A tool can hide its declaration without denying internal execution.
The SDK does not provide a general registration-time lifecycle predicate or a backend-only exposure value.
Do not edit generated SDK copies to add those contracts. Use maintained host patches if required.

## Goal request evidence

`extensions/goal/exposure_test.go` captures mock-provider requests.
Before the change, `goal_finish` appeared without a goal and remained after completion. Both regression tests failed.
After the change, the first request omits it without a goal. A working goal includes it. The completion follow-up omits it.
`go test ./extensions/goal -count=1` and `go vet ./extensions/goal` passed.

These tests do not measure tokens or qualify every provider, session branch, or concurrent lifecycle transition.

## Settings

Checkboxes control permission, not schema activation. Deferred permissions no longer force direct declarations.
Inactive and omitted entries keep their saved permission state. Explicit presets apply all listed choices.
STATE separates direct, defer, loaded, conditional, and backend states. The detail pane shows the full exposure label.
The menu omits wrapper-owned search backends except disabled recovery entries. Other MCP tools remain independent.
User-only skills are omitted even when disabled. Their user commands and saved permissions remain unchanged.
The help pane separates discoverable schemas from current schema estimates and loaded skill bodies.
Loaded bodies from user-only skills are still counted. Old messages are not removed.
Schema estimates exclude provider wrappers and shared instructions. They are not exact provider token counts.

Audited direct exceptions: url_context keeps its existing model-dependent visibility. ask_user_question keeps model-only exposure to preserve interactive input and nested-call restrictions.

## Rollout gates

1. Separate permission changes from activation changes.
2. Verify conditional declarations and stale-call rejection.
3. Add permission-aware discovery and request-bound activation expiry.
4. Verify backend execution without direct declarations.
5. Compare settings counts with actual provider requests.

Do not report hidden tools as free of all context cost. Old messages, search results, and tool results can still mention them.
