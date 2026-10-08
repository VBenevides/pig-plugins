# TODO Evidence and Commit Format

This reference defines the durable record produced by the skill.

## Per-item lifecycle

```text
TODO item
  ↓
implementation + tests
  ↓
verification passes
  ↓
implementation commit
  ↓
capture implementation SHA
  ↓
TODO evidence update
  ↓
evidence commit
```

If `TODO.md` is ignored directly or through a parent directory, or is
otherwise untracked, update it in place and leave it untracked; skip the
evidence commit. Detect ignored paths with
`git check-ignore -q --no-index -- <selected TODO path>`.

## Implementation commit

```text
type(system): short imperative phrase

- Optional implementation detail
- Optional test/detail line
```

Examples:

```text
feat(auth): add staged Codex account login

- Authenticate profiles in an isolated CODEX_HOME
- Preserve the currently selected live account
```

```text
fix(usage): ignore repeated cumulative token snapshots

- Attribute only positive cumulative deltas
- Add regression coverage for repeated last_token_usage events
```

## Evidence in TODO.md

```markdown
- [x] <original TODO text>
  - Importance Level: High
  - Description: <what changed>
  - Test Description:
    - `<command 1>`
    - `<command 2>`
  - Test Result: PASS — <specific observed result>.
  - Commit Hash: `<full SHA>`
```

A one-command verification may be kept on one line:

```markdown
- [x] <original TODO text>
  - Importance Level: High
  - Description: <what changed>
  - Test Description: `npm test -- accountRepository.test.ts`
  - Test Result: PASS — 18 tests passed.
  - Commit Hash: `<full SHA>`
```

## Evidence commit

```text
docs(system): record TODO evidence for <short phrase>

- Record verification result
- Link implementation commit <full SHA>
```

## Blocked item

Do not check it off:

```markdown
- [ ] <original TODO text>
  - Blocked: <specific reason and what is needed to unblock it>.
```

## Why two commits?

A Git commit cannot stably record its own hash in a tracked file. The hash is calculated from the commit contents and metadata, so inserting the hash into `TODO.md` and amending changes the hash again.

The implementation commit is therefore created first. Its final SHA is then
recorded by a second documentation/evidence commit.
