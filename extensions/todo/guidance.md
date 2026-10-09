<Task_Management>
## Phased Todo Management

Use `todo` for work with three or more distinct steps, an explicit task list, or new instructions during ongoing work. Capture every supplied plan item as its own task before proceeding; do not summarize away items or track them only from memory.

The tool accepts one operation per call. Reference tasks by their exact content and phases by their exact name, never IDs such as `task-1`. Keep strings stable and unique. Prefer short noun phrases for phase names without numbering, and task content that says what needs doing.

- `init`: replace the full list with `list: [{phase, items: string[]}]`, or use `items: string[]` for a single Tasks phase.
- `start`: pass `task` to change the active task; the previous active task becomes pending.
- `done`: pass `task` or `phase` to complete verified work immediately.
- `drop`: pass `task` or `phase` to abandon work that is no longer needed.
- `append`: pass `phase` and `items` to add work; a missing phase is created.
- `rm`: remove a task or a phase's tasks; omitting both targets clears all tasks.
- `view`: read the list when the exact reference text is uncertain.

Keep one task in progress. When none is active after a mutation, the earliest pending task in phase order auto-promotes. Out-of-order completion can move the pointer back to earlier open work; completed tasks stay completed. Prefer completing phases in order.

Batch todo calls with actual reads, edits, or verification where practical; avoid turns devoted only to bookkeeping. Do not mark a task done before its required output is durable and its checks pass. Report failures and blockers honestly; append follow-up work or drop obsolete tasks rather than fabricating success.

Evidence: inspect edited files and diagnostics; require exit code 0 for builds; require passing tests or explicitly report existing/environmental failures.
</Task_Management>
