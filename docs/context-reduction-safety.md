# Context reduction safety

Result pruning changes eligible observation text. It does not change assistant reasoning, call arguments, or call identifiers.
A signed result or an opaque result stays unchanged. The same check applies when a cached decision is reused.
Signed assistant reasoning does not prevent safe masking of a separate, ordinary result.
The signed assistant message stays intact.

Skill pruning still requires recorded lifetime and activation provenance.
Tool text cannot finish a skill, create a checkpoint, or supply trusted scope metadata.
Recovered events remain ordinary, untrusted tool results. See [historical recovery](context-recovery.md).

## Folding gate

`FoldCompatibility` is the required gate for future folding decisions.
Folding remains off until its separate implementation item is complete.
The initial contract permits only plain OpenAI Chat Completions interactions with complete, trusted scope metadata.
Other APIs require provider-specific tests before support can expand. They keep normal host compaction.

The scope includes session, checkpoint ancestry, provider, model, API, endpoint, and opaque credential identity.
A missing or changed value rejects reuse. An endpoint with embedded credentials or query parameters also rejects reuse.
Do not use an API-key label, email address, token, or a constant such as `api-key` as credential identity.

The pinned SDK does not supply reliable opaque identity for every credential source. Keep folding disabled when identity cannot be verified.
Never infer that identity from tool output or a prior model response.

The gate rejects signed reasoning, provider-owned replay identifiers, opaque state, generation replay vetoes, and media.
It also rejects user, system, and developer messages. Their instructions cannot become folded notes.
A caller must prove checkpoint ancestry before using the scope hash.
Scope validation does not replace balanced call/result checks or explicit completion checkpoints.

## Inspected contracts

The inspection used PiG `v0.5.1` and this repository's provider-recovery patches.
The relevant sources are `ai/messages.go`, `ai/types.go`, `ai/openai.go`, and `ai/anthropic.go` in that module.
The host patch preserves generation replay vetoes through error wrapping and serialization.
The SDK source defines signed text, thinking, and tool-call fields that the guard protects.
This inspection and local fixtures do not establish compatibility with every live provider endpoint.
