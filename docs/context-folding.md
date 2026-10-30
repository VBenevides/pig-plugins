# Folding engine

`internal/contextprune/fold.go` replaces a closed range of completed tool calls with one short note.
The engine is a library only. No extension calls it, so folding stays off.

## Rules

- The caller names every call ID of the range. A range that splits a parallel call group is rejected.
- Each call needs exactly one result, a verified recovery handle, and a matching source digest.
- The range must pass `FoldCompatibility`. Only plain OpenAI Chat Completions with a verified scope can fold.
- Failures, images, signed or opaque state, user messages, and the `todo`, `goal`, and `context_recover` tools stay in the view.
- Messages at or after `protectedFrom` stay whole.
- The note lists tool names, file paths, result sizes, duplicate results, and recovery handles. It does not call a model.
- Assistant text in the range stays. The tool calls and results go.
- A fold that would not reduce the range is rejected.
- A repeated request returns `ErrNothingToFold`. The input never changes.

Use `context_recover` with a handle for the exact saved call or result. Recovery never runs a tool.
After a fold of `read`, `edit`, or `write`, the note tells the model to read the file again before editing.

## Not yet wired

An extension needs trusted values for the provider endpoint and the credential identity.
The extension SDK does not expose them for API-key providers.
An extension also needs a user checkpoint to name the range.
