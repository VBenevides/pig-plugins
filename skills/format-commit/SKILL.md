---
name: format-commit
description: "Format implementation and TODO evidence commit messages with the project type(scope) style. Use it to create or review messages. Do not manage history."
---

# Format Commit

Use this format:

```text
type(system): short imperative phrase

- change 1: description 1 - reason 1
- change 2: description 2 - reason 2
- change 3: description 3 - reason 3
```

Select an applicable type: `feat`, `fix`, `docs`, `refactor`, `test`, `build`,
`ci`, `perf`, or `chore`. Replace `system` with the affected project component.
Use a short imperative subject.

If implementation or test details help, add a blank line and short bullets.

## Commit message transport

Build multiline messages with physical line breaks. For every multiline commit,
use `git commit -F -` with a quoted heredoc:

```sh
git commit -F - <<'EOF'
type(system): short imperative phrase

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
