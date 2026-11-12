# Offline edit and AST failure review

Source traces and hashes are listed in `prompt-baseline-2026-11-12.json`.
`prompt-tool-failures-2026-11-12.json` records each affected call, its relative trace path, line, diagnostic, and classification. No benchmark was rerun.

| Condition | Invalid arguments | Stale anchors | Undisplayed source | Native text mismatch | Ambiguous native text | Missing ast-grep |
|---|---:|---:|---:|---:|---:|---:|
| Stock pig | 0 | 0 | 0 | 3 | 6 | 0 |
| default-benchmark | 9 | 3 | 0 | 0 | 0 | 3 |
| minimum | 12 | 6 | 2 | 0 | 0 | 0 |

All 41 reported edit errors and three AST errors have direct diagnostic evidence. These categories describe observed rejection reasons. They do not prove why the model produced an argument or whether a different instruction style would avoid it.

## Findings

1. The three AST calls failed because `ast-grep` was absent from PATH. Each returned `dependency_unavailable`. This is an optional dependency limit, not evidence that parser search is incorrect. Keep that error observable. Install the dependency only as a separate experiment change.
2. Distribution edit validation failures include missing per-operation `op`, invalid anchors, malformed version tokens, and empty `lines` on delete operations. Each operation requires its own `op`. Delete omits `lines`; an empty array is not a substitute. A schema rejection must not be bypassed with Bash or write.
3. Nine distribution calls rejected stale anchors. Two rejected undisplayed source. Retain these checks. Refresh the read and use the returned version and complete displayed anchors. The trace diagnostics alone do not establish every filesystem transition.
4. Stock native edit rejected three exact-text mismatches and six ambiguous matches. Hashline-specific recovery rules do not apply to that native implementation. Prompt style must preserve this distinction.

The review confirms no new code defect that justifies weakening validation. Existing isolated tests cover missing AST dependencies, malformed inputs, stale reads, bounded source authority, and successful neighboring operations. This change adds evidence, not a speculative tool fix.

## Prompt consequences

Put one short edit contract in the primary instructions. Explain the operation shape without repeating it in all three prompt files. Keep scope, permissions, untrusted-content handling, and error reporting. Put optional durable-pipeline guidance in a separate reference document. Do not infer token savings or benchmark quality from these classifications.

Bash nonzero exits and read errors are outside this specific edit/AST review. They must not be counted as confirmed tool defects without a separate review. No live provider, image replay, patch grading, or causal experiment ran.
