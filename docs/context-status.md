# Context status

`/context-prune status` now reports these lines. Existing command forms stay the same.

- **State and policy:** on or off, cadence, protected groups, and minimum savings.
- **Budgets:** the old-failure, old-result, and old edit and write payload byte budgets, and the 75 percent post-batch target.
- **Composition:** message bytes by category in the last view: user, assistant, call arguments, results, failures, recovery notices, and images.
  Fixed instructions, tool schemas, and media payloads are excluded. These are bytes, not tokens.
- **Results:** how many results are reduced, and how many bytes stay whole in the newest messages.
- **Last request:** the estimate from the request observation. It is a byte-based estimate, not a billed-token count.
  A request with media or opaque state gets a lower-bound label. A host without patch `0014` shows "not measured".
- **Folding:** off. The engine exists in `internal/contextprune/fold.go`, but no extension calls it. See `docs/context-folding.md`.
- **Fallback:** one reason with one action when pruning is off, recovery failed, or recovery is not ready.

The status shows no payload text, credentials, or session paths.
The status cannot show whether pruning, folding, or compaction relieved pressure. That needs the admission coordinator.
No strict cap exists. Fixed instructions or one protected unit can exceed any budget.

Pruning is on by default. Add keys to `$HOME/.pig/agent/context-prune.json`. Other keys stay as they are.
See `docs/context-error-pruning.md`, `docs/context-result-budget.md`, and `docs/context-recovery.md`.
Skill lifetime commands remain under `/context-prune skill`.
