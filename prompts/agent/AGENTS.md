## Naming and structure

Use descriptive names and the language's normal conventions.

When they improve clarity:

- current-iteration values: `it_`
- cumulative values: `cum_`
- previous/current contrasted state: `prev_` / `curr_`
- temporary intermediates: `tmp_`

Do not apply prefixes mechanically.

Counters should communicate scope, for example `batch_failures`, `failures_total`, `batch_processed`, `processed_total`, and `remaining`.

Prefer guard clauses over deep nesting. Keep functions focused, but do not split straightforward logic into tiny helpers without a concrete readability, reuse, or testing benefit.

## Errors and observability

Handled errors must remain observable. Preserve the original error when wrapping it and include enough context to identify the failed operation and relevant item.

Do not suppress exceptions with empty `except/catch` blocks. If a layer cannot meaningfully handle an error, propagate it.

For independent batch items, isolate item-level failures when safe, record the reason, continue successful neighbors, and retry only through an explicit bounded policy.

Distinguish item-level failures from fatal/system failures. Stop when continuing could corrupt state, violate prerequisites, weaken security, or produce misleading output.

Never log secrets, tokens, passwords, private keys, or sensitive payloads.

## Durable and long-running work

When work produces durable output over many items:

- persist useful progress incrementally;
- resume only missing, pending, retryable-failed, or changed work;
- skip successful unchanged work;
- make retries and resume idempotent;
- avoid duplicate inserts, writes, uploads, messages, or other side effects;
- record useful failure state where practical;
- mark success only after required output is durable;
- keep destructive cleanup separate from constructive processing;
- bound concurrency, retries, queues, memory, reads, and open resources.

For multi-stage pipelines, keep stage validity explicit. A record existing in storage does not imply previous processing succeeded.

## Testing and validation

For behavioral changes, add or update tests when practical. Prefer tests of observable behavior over implementation details.

When relevant, test:

- edge and failure cases;
- one failed item alongside successful neighbors;
- interruption/resume behavior;
- idempotency and duplicate-side-effect prevention;
- compatibility of changed interfaces/configuration.

Before finishing, run the most relevant available tests plus formatter, linter, and type checker when appropriate. Report checks that could not be run.

Do not weaken existing tests merely to get a green result.

## Refactoring, dependencies, and generated files

Avoid unrelated refactors. Small local refactors are fine when they materially simplify the requested change.

Before adding a dependency, check whether the repository or standard library already provides what is needed.

Prefer:

1. an existing project pattern;
2. existing dependencies;
3. a small local abstraction;
4. a new dependency or architectural component.

Do not manually edit generated files unless required. Change the source and regenerate through the project's normal process.

## Security

- Never hardcode credentials or secrets.
- Validate untrusted input at boundaries.
- Use parameterized database queries.
- Avoid constructing shell commands from untrusted input.
- Do not disable TLS/certificate verification without an explicit requirement.
- Apply least privilege.
- Do not weaken existing security controls for convenience.

## Performance

Do not optimize without evidence. Avoid obvious repeated expensive work, unnecessary network/database calls, repeated parsing, unnecessary full-file/full-dataset reads, and reprocessing successful unchanged work.

Prefer algorithmic improvements over micro-optimization unless measurements justify otherwise.

## Agent workspace

Use `.agent-work/` for agent-only material such as:

```text
.agent-work/
├── analysis/
├── debug/
├── plans/
├── reports/
├── scripts/
└── tmp/
```

Do not clutter the project root with temporary agent artifacts.

Before completion, remove temporary debug code, experiments, placeholder code, unused imports/variables, and ad-hoc scripts that are no longer needed.
