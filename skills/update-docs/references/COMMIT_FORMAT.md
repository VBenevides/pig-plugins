# Bundled documentation commit format

This local reference carries the message rules from this project's `format-commit`
skill so `update-docs` does not require that separate skill or internet access.
Use it to format this workflow's authorized documentation commit only. It does not
create authority to commit unrelated work, bypass checks or push changes. Repository
instructions and host controls still apply. If a separately available `format-commit`
skill is consulted, do not fetch it or invoke a gated workflow just for formatting;
this bundled reference is sufficient.

## Subject and optional body

```text
docs(component): short imperative phrase

- change 1: description - reason
- change 2: description - reason
```

Use `docs` as the type, the affected project component as the scope, and an imperative
subject. Omit the body if unnecessary. Include only changes actually made; never
fabricate test evidence. Examples:

```text
docs(architecture): describe storage ownership and request flow
```

```text
docs(cli): document configuration and recovery

- usage: align examples with implemented flags - avoid stale instructions
- resources: map local state ownership - clarify recovery boundaries
```

## Breaking changes

For a breaking change, append an uppercase `BREAKING CHANGE:` footer as the final
footer after a blank line. Explain the incompatibility and required migration.
Put any other footers before it; omit it for non-breaking documentation changes.
Any commit type can describe a breaking change. Prefer the footer without `!`.
If `!` is also used, it belongs immediately before the subject colon, as in
`docs(component)!: ...`, never at the end of the message, and the explanatory final
footer is still required by this project's convention.

## Message transport and verification

For a multiline message, use physical newlines with `git commit -F -` and a quoted
heredoc; do not put escaped newline text into a `-m` argument:

```sh
git commit -F - <<'EOF'
docs(architecture): describe storage ownership and request flow

- diagrams: map implemented calls and persistence - clarify component boundaries
EOF
```

This is a message example, not permission to stage or commit. First complete the
skill's verification, explicit-path staging and unrelated-work/index checks. Never
bypass hooks, commit secrets or make an empty commit merely to finish the workflow.
Git or identity unavailability blocks the commit, not the earlier documentation work;
retain the edits and report the exact blocker. Hooks must not trigger network access
in offline mode; if required checks cannot run offline, stop rather than bypass them.

After a successful commit, inspect the actual message and paths:

```sh
git log -1 --format=%B
git show --stat --oneline HEAD
git rev-parse HEAD
git status --short
```

Report the real SHA only after success. Do not push. The optional background standard
is [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/);
all required rules for this workflow are included locally, so reading the website is
not required.
