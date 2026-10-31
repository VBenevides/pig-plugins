# Request measurement

Both context extensions use `internal/compactlimit.MeasureRequest`.
The patched host emits `provider_request_observed` after all `before_provider_request` handlers.
Each observer gets a private JSON copy. Observer return values cannot change the request.
The hook runs for each adapter request, not only at user input or turn completion.

The measurement covers the complete adapter payload. This includes system instructions, tool schemas, arguments, results, notices, and JSON framing.
It does not store the payload. It stores a fingerprint, serialized byte count, text estimate, output reservation, and media warning.
The fingerprint includes the session, branch, provider, model, and canonical payload.
It is not a byte-level prefix-cache fingerprint.

No matching provider tokenizer is available through the pinned SDK.
The initial text estimate uses one token per serialized UTF-8 byte. It is deliberately conservative, not a billed-token measurement.
Media and opaque provider state have unknown token costs. These requests cannot claim a complete token bound.
A zero output reservation means the adapter did not expose a recognized positive limit.
Output reservation and prefix-cache hits are not subtracted from prompt size.
Each request gets a new measurement. Prior pruning savings do not carry into its size calculation.

## Admission boundary

This hook can observe a request. It cannot safely wait for `Context.Compact`.
The hook runs on the agent path. Manual compaction aborts and joins that same path.
The reduction coordinator needs a host-owned pause and restart boundary before provider dispatch.
That boundary must rebuild context after a successful reduction. It must preserve cancellation and allow only one continuation.
Do not call manual compaction synchronously from this observer.

The existing `context` hook can reduce historical messages before adapter conversion.
Later context handlers and payload handlers can still change the request.
The terminal observation therefore measures the current candidate again instead of subtracting earlier savings.
Provider adapters can still apply transport-specific normalization after the payload hook.
The measurement is an adapter-payload estimate, not a claim of exact transport serialization or exact tokenization.

The host patch is `patches/pig/0014-request-observation-host.patch`.
Existing hosts without this patch keep their current behavior and do not emit observations.
Build the patched host through `scripts/dev_build.sh` before using these observations for admission decisions.
