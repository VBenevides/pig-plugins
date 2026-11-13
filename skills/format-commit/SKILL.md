---
name: format-commit
description: "Format commit messages with the project type(scope) style. Explicit invocation also creates the requested commit unless the user requests message-only output."
---

# Format Commit

## Invocation and commit authorization

Explicit user invocation of `/skill:format-commit` authorizes a commit for the requested changes.
If the user requests message-only output or review, do not commit.
Loading this skill from another workflow does not authorize an additional commit.
Follow that workflow's commit scope and checkpoints instead.

## Commit procedure

1. Inspect the repository instructions, branch, and `git status --short`.
2. Inspect `git diff` and `git diff --cached`.
3. Identify the requested changes. If their scope is unclear, ask the user before staging.
4. Preserve unrelated changes and existing staged work. Do not include them in the commit.
5. Run relevant checks. Report failures and unavailable checks. Do not claim checks that did not run.
6. Stage only the requested paths or hunks. If unrelated staged work prevents isolation, stop and ask the user.
7. Review the staged diff for scope, secrets, and accidental files.
8. Format the message with the rules below. Create the commit with the prescribed transport.
9. Verify the message with `git log -1 --format=%B`. Check `git status --short`.
10. Report the commit hash, verification results, and remaining changes.

Do not create empty commits. If no requested changes remain, report that result.
Do not amend commits, rewrite history, push, or bypass hooks without separate user authorization.
If a check or commit fails, report the cause. Do not conceal the failure.

## Required output writing

Apply `ste-writing` to all new commit-message prose and output explanations.
Read [references/STE_WRITING.md](references/STE_WRITING.md) before writing. The bundled rules work offline.
Use strict mode for instructions and safety text. Use STE-flavored mode for descriptive commit text.
Complete the required writing review before returning a message.
Preserve the required message syntax, technical names, hashes, and literal evidence.

Use this format:

```text
type(scope): short imperative phrase

- change 1: description 1 - reason 1
- change 2: description 2 - reason 2
- change 3: description 3 - reason 3
```

Select an applicable type: `feat`, `fix`, `docs`, `refactor`, `test`, `build`,
`ci`, `perf`, or `chore`. Replace `scope` with the affected project component.
Use a short imperative subject.

If implementation or test details help, add a blank line and short bullets.

## Breaking changes

Follow Conventional Commits 1.0.0: https://www.conventionalcommits.org/en/v1.0.0/

For every breaking change, use an uppercase `BREAKING CHANGE:` footer as the
final footer, at the end of the commit message. Separate the footer section from
the body with a blank line. Put any other footers before this footer. Describe
the incompatibility and the required migration. Omit this footer for non-breaking
changes; any commit type can introduce a breaking change.

```text
feat(api): require explicit request versions

- request validation: require a version field - prevent ambiguous requests
- tests: cover missing versions - verify rejection behavior

BREAKING CHANGE: Requests without a version field are rejected. Add a version field to every request.
```

The optional `!` marker cannot go at the end of the message. Conventional
Commits requires it immediately before the subject's colon:
`type(scope)!: short imperative phrase`. Prefer the final `BREAKING CHANGE:`
footer instead of `!` for this project's fixed-position convention. If `!` is
also used, keep the final footer to explain the incompatibility and migration.

## Commit message transport

Build multiline messages with physical line breaks. For every multiline commit,
use `git commit -F -` with a quoted heredoc:

```sh
git commit -F - <<'EOF'
type(scope): short imperative phrase

- change 1: description 1 - reason 1
- change 2: description 2 - reason 2
EOF
```

Keep each message line on its own source line between the `EOF` markers. Do not
put an escaped newline or the whole multiline message in one `-m` argument.
That transport stores escape text instead of line breaks.

Verify with:

```sh
git log -1 --format=%B
```
