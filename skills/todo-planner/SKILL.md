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
2. Read the selected TODO if it exists. Preserve old unchecked items and their
   evidence, and archive existing checked items with their evidence using the
   procedure below. Keep `TODO.md` for old unchecked items and new items only.
3. Put each item in exactly one section:
   - `Features` for new behavior and enhancements.
   - `Security Patches` for security risks and fixes.
   - `Bug Fixes` for defects and edge cases.
4. Order items in each section by importance: Critical, High, Medium, Low.
5. Use a short and clear title and description.
6. Do not invent evidence.
7. Create or update the selected TODO path.

## Archive completed items

Archive beside the selected TODO file (by default in
`.agent-work/todo-planner/`). This changes planning updates, not solver
checkpoints: do not archive items during implementation merely because they
became complete.

1. Identify checked items (`[x]` or `[X]`) and capture their full descriptions,
   test evidence, commit hashes, and other attached fields. Keep section labels
   and enough source context to interpret the archived evidence. Do not discard
   pending items, blockers, or unrelated notes.
2. If there are no completed items, do not create an archive. Keep all pending
   items and merge the supplied new work without duplicating existing items.
3. List existing `TODO-X.md` archives, where X is a positive integer. With none,
   use `TODO-1.md`; otherwise use one more than the highest existing number.
   Do not fill gaps: archives 1 and 3 mean the next archive is 4.
4. Create the new archive without overwriting any existing file. Preserve checked
   states and evidence verbatim; never invent missing test results or hashes.
   For nested checklists, archive completed subtrees only when they contain no
   pending descendants. Retain pending aggregates with their pending children,
   and retain parent context for completed children moved into the archive.
5. Read back the archive and verify every completed item and its evidence before
   removing that item from the selected TODO. If creating or verifying the
   archive fails, stop and report the failure; leave the original TODO unchanged.
   If the chosen filename is occupied, rescan and choose the next number rather
   than replace the file.
6. Update `TODO.md` to retain old unchecked items plus the new items, grouped and
   prioritized by the workflow categories and importance levels. Link the archive
   so completed evidence remains findable.
   Preserve relevant introductory notes and prior archive links.
7. If interrupted after archive creation but before the TODO update, compare the
   saved archive with the still-present completed items. Reuse that verified
   archive to finish the move instead of archiving the same items again. Do not
   remove items unless their complete evidence is durable. A repeated planning
   update with no newly completed items must not create another archive.

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
Keep completed items checked with their evidence in the numbered archive, not
in the active TODO. Keep pending items unchecked with their existing evidence.

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
Confirm that each moved completed item and all its evidence are in a verified
archive, and no pre-existing archive changed.
Confirm that the active TODO retains every old unchecked item and the supplied
new items exactly once, with no completed items left after a successful move.
Check the no-completed, mixed-state, existing-number/gap, repeated-update,
interrupted-update, and archive-write-failure cases against the procedure.
