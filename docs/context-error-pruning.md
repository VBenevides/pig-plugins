# Historical error pruning

Old failures no longer have a permanent full-body exemption.
The policy preserves failure status, tool identity, diagnostic head and tail text, and an exact recovery handle.
It does not infer that a later success or an assistant final response resolved the failure.
Recent failures, signed content, non-text content, and results without verified saved sources remain protected.

The existing context-prune settings accept these optional fields:

```json
{
  "historicalErrorBytes": 32768,
  "errorExcerptBytes": 3072
}
```

Merge these keys into the existing settings file. Do not replace other keys.
The default path is `$HOME/.pig/agent/context-prune.json`.
`PIG_CODING_AGENT_DIR` can select a different agent directory.

The aggregate budget accepts 4,096 through 16,777,216 bytes.
The excerpt limit accepts 512 through 16,384 bytes. Invalid values disable pruning with a diagnostic.

## Reduction behavior

The budget covers old, unprotected failure text in an accepted batch.
Each excerpt includes its omission notice and recovery handle within its byte allocation.
Failure flags and other message metadata stay unchanged. Text-block metadata also stays unchanged.
UTF-8 boundaries remain valid. Saved events stay intact.

A useful aggregate-budget reduction can bypass the ordinary 32,000-byte minimum saving.
The recent-window and batch-cadence protections still apply.
Each failure keeps at least a small diagnostic excerpt. Protected or unrecoverable content can prevent the aggregate target from fitting.
The complete request still needs measurement and normal host compaction when reduction is insufficient.
This result-text budget is not a strict total-request token cap.

Cached decisions bind to the original text and failure status.
Changed diagnostics invalidate the decision instead of inheriting old savings.
Repeated reductions preserve the original source identity and count only incremental savings.
Use `context_recover` for the exact saved event. Recovery never reruns a failed command.
