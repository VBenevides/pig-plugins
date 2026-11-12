# Offline prompt baseline: 2026-11-12

Source: `/home/wdtg/Projects/harness-benchmark/.agent-work/REPORT-2026_11_12_20h_31m.md`.
Result root: `benchmark/results/swepro-luna-medium-five-v2` in that repository.
No new model requests, benchmarks, or grading runs were made for this audit.

## Frozen evidence

- Existing traces: 45, with 15 per condition. The committed JSON inventory records every trace hash, system-record line, section hash, tool-name set, and schema hash.
- `state.json`: `6fd5144a45d525f99d05d35c198f40436fece4f18792d455dbe46a63ef1d9ffb`.
- `summary/summary.json`: `c7a1c36e39f1745bbe5d048acc2b8a59ce6b6470f3cb3de002e7208e942472f7`.
- Inventory: `prompt-baseline-2026-11-12.json`. Regenerate it with `go run ./cmd/prompt-audit -root <existing-result-root>`. The command reads saved traces only. Its JSON is deterministic.

The recorded model was `openai-codex/gpt-6-luna`, with medium reasoning. Dataset revision was `66f92766bba642462d4bbe5479e83f91f9211862`. The original run used three attempts on each of the following five tasks:

```text
instance_NodeBB__NodeBB-4327a09d76f10a79109da9d91c22120428d3bdb9-vnan
instance_NodeBB__NodeBB-0e07f3c9bace416cbab078a30eae972868c0a8a3-vf2cf3cbd463b7ad942381f1c6d077626485a1e9e
instance_ansible__ansible-ea04e0048dbb3b63f876aad7020e1de8eee9f362-v1055803c3a812189a1133297f7f5468579283f86
instance_ansible__ansible-622a493ae03bd5e5cf517d336fc426e9d12208c7-v906c969b551b346ef54a2c0b41e04f632b7b73c2
instance_element-hq__element-web-d405160080bbe804f7e9294067d004a7d4dad9d6-vnan
```

Recorded stock binary SHA-256: `29fe3e2793fbed7cf367359d5712288d468f9ff8ce50ca56357b5602356e7618`.
Recorded distribution binary SHA-256: `7e929f07cd230524e9d5e196347a24c40d411bf700b6db1ca2abcbdb7f2f8234`.
Recorded asset hash: `f81f330b39fac7f535d0f37abea3acc9edd66f80661c0d8e006a38dbf308ce52`.
The source checkout was dirty. A commit SHA alone cannot reconstruct this binary and its assets. These are recorded provenance values, not a claim that this audit rebuilt them.

## Request evidence

| Condition | Initial section characters | Initial tools |
|---|---:|---:|
| Stock pig | 2,655 | 4 |
| default-benchmark | 12,773 | 15 |
| minimum | 11,435 | 4 |

The default-benchmark traces initially declared read, edit, write, bash, tool_search, ast_search, code_search, code_overview, context_recover, and six LSP tools. This is the actual recorded declaration set, not the intended small direct core. Tool removal alone cannot explain the remaining section size in minimum. Do not change exposure while measuring a prompt-only candidate.

Section character counts exclude tool schemas. Canonical JSON hashes are evidence identities, not hashes of exact provider wire serialization. Tool additions and removals are replayed in order. Transcript system records are not independent provider-request counters. Provider usage, cached input, output, and billed cost must remain separate from character-based estimates.

## Controlled offline preparation

Use clean temporary agent homes with no credentials, user skills, saved settings, or ambient instruction files. Pin the same binary, tool declarations, tool profile, task revision, model, reasoning level, and limits for both prompt styles. Only instruction assets may differ. Record hashes of each installed style and the rendered sections before any separately authorized live run.

The saved top-level configuration does not establish all per-task time, token, dependency, and image limits. Those values must be recovered from task and trial manifests before a new run. Fresh-image replay was not present in the supplied report. No official benchmark-quality claim is supported by this small pilot.

Keep successful unchanged trials when resuming. Do not run a pilot, held-out evaluation, fresh-image grading, or model request until the user supplies separate approval and limits.
