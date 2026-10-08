---
name: todo-solver
description: "Execute all actionable items in TODO.md. Implement, test, commit, and record evidence for each item. Use ONLY when the user explicitly invokes /skill:todo-solver; never activate from task relevance or natural-language requests. Do not use it for planning or isolated changes."
disable-model-invocation: true
---

# TODO Solver

Activation gate: Execute this skill ONLY with the host's explicit user-invocation provenance for `/skill:todo-solver`, or expressly authorized delegated context carrying that provenance and assignment. The literal command need not survive in the request. Loading `skill://todo-solver`, mentions, configuration, automatic loading, recommendations, task relevance, and natural-language requests alone do not authorize execution.

Execute the caller-specified TODO file until no executable item remains.
Otherwise use `.agent-work/todo-planner/TODO.md`.
Stop only when a real blocker prevents more work.

## Required progress checkpoint

Complete this checkpoint after each actionable item.
Do not wait until the end of `TODO.md`.

1. Implement the item and add or update its tests.
2. Run the required verification.
3. Load `/skill:format-commit` for the message format, then commit the
   implementation and tests under this workflow's commit authorization.
4. Capture the implementation commit SHA.
5. Update `TODO.md` with the item state, test result, and SHA.
6. Before deciding whether to commit `TODO.md`, run
   `git check-ignore -q --no-index -- <selected TODO path>`. A successful
   check includes both direct and parent-directory ignore rules. If ignored,
   update it in place with the implementation SHA, but do not add or commit
   it. Otherwise, if `TODO.md` is tracked, commit its update as a separate
   evidence commit using `/skill:format-commit`; if untracked, update it in place
   only.
7. Start the next item only after these steps finish.

Do not combine implementation commits or TODO updates.
If an item is blocked, record the blocker in `TODO.md`.
If `TODO.md` is tracked and is not ignored, commit that update before you
continue with independent work. If it is ignored or untracked, update it in
place and leave it untracked.

## Startup

Before editing:

1. Find the repository root.
2. Read the applicable `AGENTS.md`, `PLAN.md`, `PLANS.md`, `TODO.md`, and
   project configuration files.
3. Run:

   ```bash
   git status --short
   git branch --show-current
   git log -5 --oneline
   ```

4. Inspect existing uncommitted changes.
5. Do not overwrite, revert, stage, or commit unrelated changes.
6. Find the normal test, lint, format, type-check, and build commands.
7. Run a baseline check when it can identify existing failures.
8. Count the actionable unchecked leaf items.
9. Map their dependencies, ownership, and safe delegation options.

If unrelated changes prevent safe isolation, use a worktree.

## TODO items

Treat an unchecked leaf checkbox as one item.

```markdown
- [ ] Implement account switching
  - [ ] Add a switch lock
  - [ ] Add rollback tests
```

The two child checkboxes are items.
The parent is an aggregate checkbox.
Mark the parent complete after its children and its own requirements are complete.

Do not create a commit only to change an aggregate checkbox.
Keep inseparable requirements in one item.
Do not split an item if the split makes the project invalid or untestable.
Follow dependency and milestone order from `TODO.md` and `PLAN.md`.

## Item workflow

### Scope

Before you write code:

- Identify the affected files and components.
- Identify the acceptance criteria.
- Find relevant tests.
- Find dependencies on unfinished items.
- Classify the item as P0, P1, or P2 for delegation.

Keep the change focused. Do not add unrelated refactors.

### Implement and test

Implement the smallest complete change that satisfies the item.

- Follow repository conventions.
- Preserve backward compatibility unless the item requires a breaking change.
- Avoid unnecessary dependencies.
- Keep public interfaces intentional.
- Keep required validation, error handling, and security controls.
- Add or update tests for behavior changes when practical.
- Do not weaken tests to make them pass.

Use unit tests for isolated logic.
Use integration tests for module interactions.
Use regression tests for bugs.
Use structural checks for build, type, lint, or format changes.
Do not add unit tests for changes to documents or metadata only.

### Verify and repair

Run the narrowest useful check first.
Run broader checks when the change requires them:

```text
focused test
→ related test suite
→ type check
→ lint or format check
→ build
```

When a check fails:

1. Decide if the failure is caused by the change, existed before the change,
   or comes from the environment.
2. Fix failures caused by the change.
3. Run the check again until it passes.
4. Record any existing or environmental limitation in `TODO.md`.

Do not hide failures by deleting assertions, skipping tests, or reducing coverage.
Only the item requirements can permit such a behavior change.

### Review and commit

Before the implementation commit, inspect:

```bash
git status --short
git diff
git diff --cached
```

Confirm that the implementation commit contains only the current item.
Check for missing tests, secrets, credentials, generated files, and local environment files.

Stage explicit paths.
Load `/skill:format-commit` and use its implementation commit format. This
workflow owns the authorized commit; the formatting skill does not manage history:

```text
type(system): short imperative phrase
```

Use the project component for `system`.
Select an applicable commit type.
See [commit-and-todo-format.md](references/commit-and-todo-format.md) for examples.

After the implementation commit, capture its final SHA:

```bash
git rev-parse HEAD
```

### Update TODO.md

Update the item immediately after the implementation commit.
Keep its `Importance Level`, `Description`, and `Test Description`.
Add:

- the completion state.
- the commands that ran.
- the observed `Test Result`.
- the full implementation `Commit Hash`.

Use `[x]` only when the item is complete.
Do not claim a passed test unless you ran it.
If `TODO.md` is tracked and is not ignored, commit the TODO update separately:

```text
docs(system): record TODO evidence for <short item phrase>
```

The SHA in `TODO.md` must identify the implementation commit.
It must not identify the evidence commit.

After the evidence commit:

1. Run `git status --short`.
2. Re-read the changed TODO section.
3. Mark required aggregate checkboxes complete.
4. Continue with the next actionable item.

If `TODO.md` is ignored or untracked, skip the evidence commit and leave the
updated file untracked.

## Git safety

- Do not use `git reset --hard` on user work.
- Do not use `git clean -fd` without a clear need and permission.
- Do not force-push unless the user asks.
- Do not rewrite unrelated commits.
- Do not use `git add .` when unrelated changes exist.
- Do not combine unrelated items into one implementation commit.
- Do not create empty commits for headings.
- Do not commit secrets, credentials, `.env` files, or local auth files.

If a hook changes files, inspect the change.
If behavior changes, run the relevant checks again.
Create another commit when needed.
Do not skip a hook only to finish the TODO.

## Blocked items

An item is blocked when it needs missing information, access, approval, hardware, or a product decision.

When an item is blocked:

1. Leave its checkbox unchecked.
2. Add a specific blocker and the required unblock condition to `TODO.md`.
3. Commit the TODO progress update.
4. Continue with later independent items when safe.
5. Report the blocker at the end.

Do not invent manual test results, platform results, API responses, or UI
observations.

## Long-running work

Keep progress in the repository.
Use `TODO.md` as the persistent record.
During long work, read the applicable plan and instruction files again.
Keep one item on the main integration path.
Continue while the environment permits useful work.

## Completion audit

Before declaring the TODO complete:

1. Use the host's search tool to find unchecked items with pattern
   `^\s*-\s+\[ \]` in the actual caller-selected TODO path (or the selected
   default `.agent-work/todo-planner/TODO.md`), not an assumed root `TODO.md`.
   If no search tool is available, read and parse that selected file for
   unchecked items.
2. Classify each remaining item as executable, aggregate, deferred, or blocked.
3. Confirm that every completed actionable item has truthful test evidence and implementation commit hash.
4. Run the broadest reasonable repository checks.
5. Run:

   ```bash
   git status --short
   git log --oneline --decorate -20
   ```

6. Confirm that no agent scratch files, credentials, or temporary worktrees
   remain.
7. Ensure `TODO.md` ends with one `## CHANGELOG` section containing:
   - `### Features` with every feature changed during this run.
   - `### Bugfixes` with every bugfix changed during this run.
   - `### Other Changes` for any remaining changes, such as documentation or
     tests.
   Use `- None.` for an empty subsection. Preserve existing changelog entries,
   update them instead of duplicating the section, and commit the final
   tracked, non-ignored `TODO.md` update as a separate evidence commit.

## Final response

Keep the response brief. Include:

- the number of completed actionable items.
- the final verification result.
- blocked or remaining items.
- the branch and working-tree status.
- the latest relevant commits.

Do not claim completion when an executable item remains unchecked.
