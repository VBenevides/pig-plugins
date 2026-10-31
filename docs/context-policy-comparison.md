# Policy comparison

This comparison is offline and synthetic. `TestPolicyComparisonOnSyntheticSession` builds the fixture.
It follows the measured content mix: large write arguments, small shell results, web results, and old failures.
It holds no private session data. No live model, billing, cache, latency, or task-success measurement ran.

Message bytes in the model view (UTF-8, excluding fixed instructions, schemas, and framing):

| Policy | Bytes |
|---|---:|
| No reduction | 169,072 |
| Existing pruning | 169,072 |
| Bounded output budgets (16 KiB each) | 90,327 |
| Folding 25 old groups, then budgets | 66,778 |

The existing rules removed nothing from this fixture. Each result is below the shell threshold, and the savings floor is 32,000 characters.
This matches the analyzed session, where small results and call arguments accumulated.

Every policy kept the user constraint, the old failure text, balanced call and result pairs, and the saved history.
The test fails when a policy loses a constraint, unbalances a pair, or changes saved history.

## Limits

- The byte counts are not provider tokens.
- Folding is not wired into an extension. The test calls the engine directly and runs it before budgets, because it verifies original digests.
- Pick defaults from live evidence. The current defaults of 32 KiB for failures and 64 KiB for results are unvalidated choices.
- Live comparison needs the same models, tasks, seeds, and request budgets. It also needs cache and billing data. None of these ran.
- Real-session replay of the 250,000-token session was not repeated here.
