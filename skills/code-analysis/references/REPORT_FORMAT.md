# Analysis report format

Use this local format without fetching its source references. Replace placeholders
with observed facts, or `Unavailable` / `Not run` with a reason. Remove instructional
placeholders before publication. Include both review areas, even if no findings were
confirmed. Keep the report at `.agent-work/code-analysis/ANALYSIS_REPORT.md`; archive
its predecessor using the skill's verification-before-replacement procedure.

## Complete report skeleton

```markdown
# Code Analysis Report

## Analysis Metadata
- Started (UTC): <YYYY-MM-DDTHH:MM:SSZ>
- Finished (UTC): <YYYY-MM-DDTHH:MM:SSZ>
- Repository and scope: <root and included paths/interfaces>
- Exclusions: <paths and reasons, or None>
- Branch: <name, Detached HEAD, or unavailable reason>
- Commit evaluated: <full SHA or unavailable reason>
- Working tree: <clean/dirty and relevant uncommitted changes>
- Revision drift: <changes during review, or None observed>
- Connectivity: <Offline / permitted online lookups actually used>
- Previous reports: <relative links to preserved archives, or None>

## Executive Summary
<Point-in-time assessment, not a warranty. Main risks and recommended priorities.
Distinguish confirmed issues from unverified hypotheses.>

## Methods and Limitations
- Inspected: <entry points, call paths, configuration and tests>
- Security coverage: <trust boundaries checked and gaps>
- Performance coverage: <workloads, resource bounds and gaps>
- Environment: <relevant OS/runtime/tool versions actually obtained>
- Unavailable checks: <reason and effect on confidence>
- Advisory freshness: <local advisory snapshot/version if available, otherwise
  latest vulnerability status not checked offline>

## Security Findings
### SEC-001: <title>
- Status / confidence: <Confirmed or Hypothesis; rationale>
- Severity: <Critical/High/Medium/Low; impact and likelihood rationale>
- Affected code: <path, symbol and optional line range>
- Observed behavior: <what happens, not just an error message>
- Evidence: <source trace, safe reproduction, or sanitized check output>
- Preconditions and impact: <required access/input and affected assets>
- Recommendation: <smallest sound mitigation and compatibility tradeoff>
- Verification plan: <scenario, expected result and local check when known>

## Performance and Optimization Findings
### PERF-001: <title>
- Status / confidence: <Measured or Hypothesis — not measured; rationale>
- Priority: <Critical/High/Medium/Low; workload/impact rationale>
- Affected code: <path, symbol and optional line range>
- Evidence: <measured bottleneck or source-backed complexity/resource concern>
- Workload and environment: <input scale, concurrency, runtime/hardware if measured>
- Measurements: <command, baseline, repetitions, units, distribution/variability>
- Impact: <latency/throughput/memory/I/O cost; clearly label estimates>
- Recommendation: <algorithm/resource improvement and tradeoff>
- Verification plan: <repeatable workload and correctness checks>

## Verification Results
| Check / command | Result | Evidence / limitation |
| --- | --- | --- |
| <actual local command or source inspection> | <Passed/Failed/Not run> | <observed result or reason> |

## Recommendations
| Priority | Finding | Proposed action | Verification / dependencies |
| --- | --- | --- | --- |
| <priority> | <finding ID> | <action, not an automatic fix> | <check or blocker> |

## References
- Code evidence: <paths/symbols or local artifacts actually inspected>
- Bundled guidance: <skill-local reference actually read>
- Optional web sources: <URL and actual UTC access date, or Not accessed (offline)>
```

## Evidence and rating rules

- Repeat finding blocks only for actual findings. If none are confirmed, write
  `No confirmed findings` under that area, explain coverage and retain any clearly
  labeled hypotheses. Do not invent findings to fill the template.
- Assign stable IDs within the report; retain IDs for the same finding when useful
  across reports. Explain severity using impact and likelihood, not a fabricated
  numerical score. Critical means a severe, credible exposure requiring immediate
  attention; High means substantial impact with plausible prerequisites; Medium
  means constrained impact/likelihood; Low means limited impact. State uncertainty.
- For performance use priority, not security severity. Do not infer measured speedups
  from code appearance. Record workload, baseline, repetitions, units and variability
  for measured claims. Without a safe baseline, state the limitation and propose a
  future measurement instead of making up a comparison. Prefer bounded existing tools.
- Distinguish a source inspection from an executed test. Retain relevant failures and
  unavailable checks. No scanner installed or no internet is not evidence of safety.
- Do not expose credentials or private payloads in evidence. Include only the minimum
  sanitized reproduction needed. Do not run risky demonstrations without permission.
- Capture UTC/Git metadata with available local tools. If the clock is unavailable,
  record that rather than inventing a datetime. Git absence, unborn branches, detached
  HEAD and worktrees are valid states; missing Git must not prevent the review.

The format adapts the reporting and measurement principles listed in
[README.md](README.md); no external format/schema validator is required.
