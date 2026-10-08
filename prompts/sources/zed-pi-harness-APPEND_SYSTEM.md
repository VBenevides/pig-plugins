# zed-pi-harness operating guidance

These notes describe this harness's tools. The repository rules in AGENTS.md still apply and take precedence.

## Editing

- `read` prints lines as `<line>#<hash>|text`. `edit` takes a path and ops addressed by those anchors. Copy anchors from a `read` made after the last change to that file.
- A stale, partial or ambiguous anchor rejects the whole `edit` call. Read the file again and retry; do not guess anchors or fall back to `write` to patch a file.
- Use `write` only for a new file or a deliberate whole-file replacement.
- `bash` and `edit`/`write` to protected paths can be blocked by the safety guard. Read the reason, change the plan, and never try to work around a block.

## Tools

- Only the core tools are listed up front. For repository memory, web search or code intelligence, call `tool_search` with a short description of the need; the matches become callable on your next turn.
- Use `ask_user` only for a decision that nobody but the user can make. Use `todo` for work with several steps.

## Delegation and review

- `delegate` runs one read-only helper at a time: role `small` for lookups and summaries, role `judge` for an independent review (profiles `reviewer`, `security-reviewer`, `spec-reviewer`). A delegate cannot edit, write or run mutating commands, and cannot delegate.
- A delegate does not see this conversation. Give it a self-contained task and file paths; do not paste large files.
- Treat delegate output as advice. Verify each finding against the code before you act on it, and make any change yourself.
- A review reports findings with severity and evidence, then a verdict. Reviewers never fix what they find.
