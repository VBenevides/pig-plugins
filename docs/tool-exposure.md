# Tool exposure

## Policy

Permission and exposure are separate states. A permission allows execution through normal hooks. Exposure determines which declarations reach the model.

| Mode | Initial declaration | Discovery | Execution |
|---|---|---|---|
| Direct | Included when active | Not needed | Normal hooks |
| Deferred | Omitted until activated | `tool_search` or codemode | Normal hooks |
| Conditional | Included only while its condition is true | Must not bypass the condition | Check the condition again |
| Backend | Omitted from direct use | Prefer wrapper-only access | Normal hooks through the wrapper |

The initial direct core is `read`, `edit`, `write`, `bash`, and `tool_search`, subject to saved permissions.
The default uses native file tools. Hashline remains an explicit setting. The minimal profile restricts permissions to the four native file and shell tools, with no skills or discovery.
Before each user request, tool-settings removes non-core activations. Discovered tools remain active through the request and are released at agent_settled.
If discovery is unavailable or disabled, only the four core tools remain exposed. Show a warning; do not activate the other tools automatically.

## Source inventory

This inventory describes the pinned PiG 0.5.1 SDK and local registrations. It does not claim that all installed MCP servers are known.

| Owner | Tools | Registered exposure | Condition or dependency |
|---|---|---|---|
| Host | `read`, `edit`, `write`, `bash` | Direct | Permissions and file implementation |
| Host and tool-search extension | `tool_search` | Model-only | Built-in discovery must be installed and active |
| Goal extension | `goal_finish` | Deferred with a loadout guard | Working goal and permission; execution checks the condition again |
| Web-search extension | `web_search`, `url_context` | Deferred | Model support and permissions |
| Web-search MCP registration | Parallel and Exa tools | Deferred MCP | Wrapper fallback and permissions |
| LSP extension | `lsp_*`, `code_search`, `code_overview` | Deferred | Language server availability |
| AST search extension | `ast_search` | Deferred | Parser support |
| Pi curator source, not bundled | Memory tools | Deferred | Explicit external loading only |
| Browser extension | Browser tools | Deferred when enabled, hidden when off | Browser configuration and mode |
| Todo extension | `todo` | Deferred | Instructions remain independent of schema exposure |
| Ask-user-question extension | `ask_user_question` | Deferred | Interactive input support |
| Context-prune extension | `context_recover` | Deferred | Use discovery before reading saved output |

`ToolInfo.Exposure` reports registration metadata. `GetActiveTools` reports activation, not permission.
`HiddenTools` alone does not establish the exact provider request or the reason a declaration is absent.
Runtime MCP inventories can change. Use actual registry metadata and captured requests for unknown tools.

## Verified host behavior

The pinned host implements discovery in `coding/extension/builtin/toolsearch/tool.go`.
It searches only `deferred` and `codemode` registrations that are not active.
It activates matches through `SetActiveTools`. Results contain names and descriptions, not complete schemas.
Direct tools are not discovery candidates. All bundled non-core tools register as deferred, or hidden while disabled.
The native search loop does not consult this project's saved permission file or a general lifecycle predicate.
The tool-search extension filters denied activations before the next declaration. It redirects verified search dependencies to web_search.
It releases only its reported discoveries at agent_settled. Pre-existing and codemode-owned activations are not removed.
Search accepts at most 4096 query bytes and 20 results. A request can activate at most 20 tools through this extension.
The host can rank denied candidates before filtering. A search can therefore return fewer allowed matches than requested.

The SDK provides `PrepareLoadout` and `HiddenDeclarations`. A tool can hide its declaration without denying internal execution.
The SDK does not provide a general registration-time lifecycle predicate or a backend-only exposure value.
Do not edit generated SDK copies to add those contracts. Use maintained host patches if required.

## Earlier goal request evidence

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

## Five-tool request checks

`extensions/tool-settings/discovery_test.go` captures initial, post-discovery, and next-request schemas with local mock providers. It covers todo, user questions, recovery, URL context, conditional goal completion, and enabled browser tools. Separate execution fixtures explicitly activate deferred tools; those fixtures are not used for discovery checks.
Tool instructions can still add context independently. Goal, todo, and ask-mode instructions remain enabled. Schema deferral does not remove old messages or tool results.

## Rollout gates

1. Separate permission changes from activation changes.
2. Verify conditional declarations and stale-call rejection.
3. Add permission-aware discovery and request-bound activation expiry.
4. Verify backend execution without direct declarations.
5. Compare settings counts with actual provider requests.

Do not report hidden tools as free of all context cost. Old messages, search results, and tool results can still mention them.
