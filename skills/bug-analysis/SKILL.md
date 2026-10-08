---
name: bug-analysis
description: Find the root cause of a defect, failure, regression, or unexpected result. Use it for errors, incidents, logs, or failed tests. Produces BUG_REPORT.md.
---

# Bug Analysis

Use evidence to find the root cause of a symptom.

## Core contract

- Write a caller-specified output path when provided. Otherwise write
  `.agent-work/bug-analysis/BUG_REPORT.md`.
- Primary output: `BUG_REPORT.md`.
- Reproduce the issue or collect evidence before you state the root cause.
- Distinguish symptom, contributing factors, and root cause.
- Do not propose a fix from an error message alone.
- First, show the cause-and-effect chain.
- `BUG_REPORT.md` owns the regression plan. Do not create a separate `TEST_PLAN.md`.

## Workflow

1. Record:
   - expected behavior
   - actual behavior
   - environment
   - reproduction steps
   - frequency and scope
2. Reproduce the issue when this action is safe and practical.
3. Inspect relevant logs, stack traces, tests, recent changes, configuration, and code paths.
4. Form hypotheses.
5. Test the hypotheses. Use the least costly evidence that can distinguish them.
6. Trace the failure to the earliest incorrect state or violated invariant.
7. Identify affected components and the probable scope.
8. Determine whether the bug is:
   - regression
   - latent defect
   - configuration/environment issue
   - dependency/platform change
   - invalid assumption
   - data issue
9. Propose the smallest correct fix direction.
10. Define each required regression test.
11. Include its scenario, expected result, test layer, and validation command.

## BUG_REPORT.md format

```markdown
# Bug Report: <title>

## Summary
## Expected Behavior
## Actual Behavior
## Environment
## Reproduction
## Evidence
## Root Cause
## Contributing Factors
## Affected Components
## Blast Radius
## Proposed Fix Direction
## Regression Tests Required
## Risks
## Open Questions
```

If evidence does not prove the root cause, write `Root Cause: Unconfirmed`.
List the main hypotheses and the evidence required to test them.

For each confirmed bug, define a regression test that fails before the fix.
The same test must pass after the fix.
If this test is not practical, explain why. Define the smallest reliable alternative check.

## Handoff

- If behavior needs clarification, load and execute `/skill:spec-writing` only when the current request or delegated context authorizes that destination; otherwise recommend the command with `BUG_REPORT.md` and the confirmed fix direction as inputs.
- Otherwise load and execute `/skill:todo-planner` only when the current request or delegated context explicitly authorizes that handoff; otherwise recommend the command with `BUG_REPORT.md` as input.
- For security-relevant defects, pass `BUG_REPORT.md` to `security-analysis` only within an explicitly authorized parent run with `security` selected. Otherwise recommend `/skill:project-analysis security` with that report as input. Ordinary security checks do not require invoking that workflow.

## Completion checks

- Evidence supports the root cause, or the report marks it as unconfirmed.
- The report gives reproduction steps when possible.
- The report identifies the affected scope.
- The report defines regression verification.
- The proposed fix addresses the cause and not only the symptom.
