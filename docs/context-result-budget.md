# Accumulated result budget

Old successful tool results no longer accumulate without a bound.
The budget covers results that no tool-specific rule reduced. It includes small shell output, web results, and unknown MCP tools.
The newest results stay whole. Older results become a short marker with the tool name, byte count, and recovery handle.
Status, details, and other message metadata stay unchanged.

Add these optional keys to the existing `$HOME/.pig/agent/context-prune.json` file:

```json
{
  "historicalResultBytes": 65536,
  "resultExcerptBytes": 128
}
```

The budget accepts 4,096 through 16,777,216 bytes. The excerpt accepts 0 through 4,096 bytes.
Invalid values disable pruning with a diagnostic.

A batch that exceeds the budget reduces results to 75 percent of it. The gap prevents a new rewrite after each small result.

## Protections

- Recent messages and recent tool-call groups stay whole.
- Failures keep the separate error budget.
- Images, signed content, and non-text content stay whole.
- A result needs a verified saved source and a recovery handle before reduction.
- A marker never replaces a result that it would make larger.
- A changed source invalidates the cached decision.

The budget bounds result text only. It does not bound call arguments or the full request.
Use `context_recover` for the exact saved result. Recovery never reruns a tool.
