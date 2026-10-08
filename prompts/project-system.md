<pig_plugins_project_rules>
# Project operating guidance

These rules adapt the original Pi Harness guidance to this project's native PiG tools.
Repository instructions in AGENTS.md still apply. Preserve the host's base prompt, project context, skills, and tool guidance.

## Hashline-only file changes

- Use `read` to inspect files. Text lines have `<line>#<hash>|text` anchors.
- For large source files, inspect the outline first. Use `offset` and `limit` for the necessary ranges.
- Make every intentional change to an existing project or workspace file with `edit` and exact anchors from `read`.
- This rule includes source, configuration, documentation, tests, and complete rewrites.
- Copy anchors from a read after the file's most recent change. Never invent anchors.
- If an anchor is stale, missing, or ambiguous, read again. Retry with fresh anchors. Never bypass validation.
- Combine planned changes to one file into one edit call when practical. Operations refer to the original read.
- Do not overlap operations or insert twice at the same position.
- Use `write` only to create a path that does not exist. Never overwrite an existing file with `write`.
- Never use shell redirection, scripts, patch commands, formatters, generators, or auto-fix commands to change project file contents.
- Never use LSP rename, code actions, AST rewrites, or another custom tool to bypass these rules.
- Normal build outputs, test artifacts, caches, downloads, and tools' own runtime state are allowed.
- Respect safety blocks. Read the reason and change the plan. Never bypass a block through another tool.

## Available tools

- Use the actual loaded tool schemas. Do not assume APIs or tools from another harness.
- `bash` remains available for shell commands, file discovery, text search, builds, tests, and non-mutating inspection.
- If dedicated search or discovery tools are loaded, prefer them. Use `read` for file contents.
- Use `tool_search` to discover deferred tools when available. Search by the task you need to perform.
- Native repository memory tools are `memory_search` and `memory_read`.
- Native web tools are `web_search` and `url_context`. Use primary sources and cite URLs.
- Native code intelligence tools use `lsp_` names. Discover the required operation before calling it.
- Use `ask_user_question` only for a decision that tools and repository context cannot resolve.
- Use `todo` for multi-step work when available. Keep one active item and keep the list current.
- Do not assume the original harness's `ask_user` or `delegate` tools exist.
- If delegation is actually loaded, give a self-contained task and paths. Verify advisory findings against the code.

## Engineering

- Inspect relevant code, callers, tests, and repository instructions before changing behavior.
- Make the smallest complete change. Preserve unrelated behavior. Do not add tools or dependencies without a task requirement.
- Never suppress failures silently. State the failure and the established fix or next diagnostic action.
- Use check-only validation modes. Apply source corrections through anchored `edit` calls.
- Report only checks you actually ran. State material failures or limits before claiming completion.
- Never expose secrets, credentials, or tokens.

</pig_plugins_project_rules>
