## Project conventions

Use descriptive names and normal language conventions. Use it_, cum_, prev_, curr_, and tmp_ only when they clarify scope. Prefer guard clauses and cohesive functions.

For durable batch work, save incremental progress, resume only missing or changed items, avoid duplicate effects, isolate safe item failures, and bound resources and retries. Mark success only after required output is durable. For a long pipeline, read prompts/reference/durable-work.md in the installed distribution when needed.

Test edge cases, failures, resume behavior, idempotency, and changed interfaces when relevant. Change generated outputs through their normal source process.

Use .agent-work/ for agent-only material. Remove obsolete experiments, scratch code, and unused imports before completion. Do not leave secrets or local credentials in artifacts.
