# Parallel Subagent Playbook

Use this reference when a TODO has enough independent work to benefit from parallel execution.

## Objective

Keep the main agent focused on orchestration and integration while subagents execute bounded independent work.

## Minimum expectation

When a suitable delegation API and safe isolation are supported:

- 4+ actionable leaf TODOs and at least 2 independent workstreams -> spawn at least 2 subagents.
- Large milestones with disjoint ownership -> prefer 3–5 concurrent subagents.
- Small TODOs -> still use read-only support agents when exploration, review, or testing can proceed independently.

On OMP, use the native exposed `task` tool with its actual schema. On Pi,
delegation requires an installed extension; use that extension's actual tool
and schema. If delegation is unavailable, execute sequentially and record the
reason. If writer isolation cannot be dispatched, implement sequentially;
read-only support can still be delegated where safe.

## Dependency map

Classify:

```text
P0 = integration-sensitive/main-path
P1 = independent implementation
P2 = independent support/review/testing
```

Example:

```text
P0: wire account switch service into extension activation
P1: implement account repository
P1: implement SQLite migrations
P1: implement rollout token parser
P2: design parser fixtures
P2: review auth-file secret handling
```

## Worktree pattern

Each writing agent requires a separate worktree or host-managed isolated
workspace. A separate branch in the same directory is not writer isolation.
Use host-managed isolation only if the exposed delegation schema supports it.
Use explicit Git worktrees only if the actual delegation mechanism can dispatch
agents into those directories; never invent `cwd` or isolation parameters.
If neither option is supported, execute implementation items sequentially.

Example shell structure, only after confirming worktree dispatch support:

```bash
git worktree add ../wt-account-repo -b agent/account-repo
git worktree add ../wt-usage-db -b agent/usage-db
git worktree add ../wt-token-parser -b agent/token-parser
```

Each agent gets one worktree and explicit file ownership.

Do not let agents modify `TODO.md` in parallel.

## Integration loop

```text
agent finishes
   ↓
main inspects diff
   ↓
integrate/cherry-pick
   ↓
rerun focused verification
   ↓
fix integration issues
  ↓
canonical implementation commit
  ↓
record SHA in TODO.md
  ↓
evidence commit
  ↓
reassign available agent slot
```

## Good agent roles

### Implementation agent
Owns a leaf TODO and tests.

### Scout
Maps files, APIs, dependencies, and risks without editing.

### Test planner
Finds the strongest focused verification and missing coverage.

### Reviewer
Inspects integrated changes for correctness/regressions.

### Debugger
Reproduces failing tests and proposes minimal fixes.

### Security reviewer
Checks secrets, permissions, input validation, and unsafe filesystem/process behavior.

## Avoid

- Multiple writers in one working tree.
- Two agents editing the same central manifest/migration.
- Letting subagents independently mark TODO items complete.
- Treating provisional worktree commits as canonical hashes.
- Spawning agents with vague goals like "work on the backend."
- Waiting for all agents to finish before integrating completed independent work.
