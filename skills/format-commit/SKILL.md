---
name: format-commit
description: "Format implementation and TODO evidence commit messages with the project type(scope) style. Use it to create or review messages. Do not manage history."
---

# Format Commit

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
