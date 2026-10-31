# Old edit and write payload budget

Large `edit` and `write` payloads made up more than half of the message bytes in long sessions.
Each payload is saved in the session file, and its content is on disk. An old copy in the request has little value.

The budget replaces the large strings in old `edit` and `write` call arguments with a short marker.
The marker names the tool and field, the size, and a `context_recover` handle for the exact saved assistant message.
The call ID, tool name, `path`, and any string under 512 bytes stay unchanged. The tool result is not changed.

Add this optional key to `$HOME/.pig/agent/context-prune.json`:

```json
{ "historicalArgumentBytes": 32768 }
```

The budget accepts 4,096 through 16,777,216 bytes. An invalid value disables pruning with a diagnostic.
A batch that exceeds the budget reduces payloads to 75 percent of it. The newest payloads stay whole.
The 32,000-character minimum saving and the pressure exception from the other budgets also apply.

## Protections

- Only calls with a successful result, outside the recent window, are eligible. A failed call keeps its payload, so a retry can use it.
- The saved assistant message needs a verified handle and a matching argument hash. A changed call keeps its original arguments.
- An assistant message with signed or opaque replay state stays unchanged.
- The saved history never changes.
- After a reduction, read the file again before an edit. A recovered payload is historical evidence and gives no edit authority.

Status shows the budget on the `budgets:` line.
