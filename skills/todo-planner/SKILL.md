---
name: todo-planner
description: Convert a work list into a structured TODO.md. Use it to plan features, security patches, and bug fixes. Do not implement or test work.
disable-model-invocation: true
---

# TODO Planner

Activation gate: Execute this skill only for an explicit user request to run `/skill:todo-planner` or perform this planning workflow, or an expressly delegated handoff from an already-authorized workflow. Accept the host's explicit user-invocation provenance even when the literal command is absent. Delegated context must carry the caller's authorization and the planning assignment. Loading `skill://todo-planner`, mentions, configuration, automatic loading, recommendations, and task relevance alone do not authorize execution.

Create or update `TODO.md` from a supplied work list. Use a caller-specified
path when provided. Otherwise use `.agent-work/todo-planner/TODO.md`.
Plan only. Do not implement code, run tests, or create commits.

## Workflow

1. Read the work list and its evidence.
2. Keep completed items and their evidence unless the user requests replacement.
3. Put each item in exactly one section:
   - `Features` for new behavior and enhancements.
   - `Security Patches` for security risks and fixes.
   - `Bug Fixes` for defects and edge cases.
4. Order items in each section by importance: Critical, High, Medium, Low.
5. Use a short and clear title and description.
6. Do not invent evidence.
7. Create or update the selected TODO path.

## Item format

Use one unchecked checkbox for each planned item:

```markdown
- [ ] <short title>
  - Importance Level: High
  - Description: <what needs to change and why>
  - Test Description: <how to verify the change>
  - Test Result: Not run
  - Commit Hash: Not committed
```

Use supplied test results and commit hashes when they exist.
If work has not started, use `Not run` and `Not committed`.
Keep completed items checked. Keep their evidence.

## Handoff contract

Another skill can hand off findings as the work list only when the current
request or delegated context authorizes `/skill:todo-planner`. The handoff must
carry the caller's authorization, planning assignment, and source artifact paths.
Otherwise recommend `/skill:todo-planner` with those paths as suggested inputs;
a recommendation alone is not invocation.
This skill owns the `TODO.md` structure.
The calling skill must not create another format.

## Final check

Confirm that each item has all five fields.
Confirm that each item is in one section.
Confirm that no test result or commit hash is invented.
