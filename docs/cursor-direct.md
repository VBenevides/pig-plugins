# Cursor direct provider (offline-tested)

Cursor now uses a native, CLI-free provider by default. This is an experimental
port of [Oh My Pi's Cursor implementation at revision
3a335e71c3c9382a92be209f90b8b52a1f7c640d](https://github.com/can1357/oh-my-pi/tree/3a335e71c3c9382a92be209f90b8b52a1f7c640d).
Its non-public protocol may change. **No real Cursor login, catalog or generation
has been tested here.** Use only an account and service access you are entitled
to use. This is not an official Cursor API.

## Try it

1. Build with `GOTOOLCHAIN=go1.27.2 ./scripts/dev_build.sh`.
2. Start `./build/pig-plugins`. Use `/login` and select Cursor if needed. Existing
   native Cursor credentials and account selection remain in the native store.
3. Open `/model` and choose a discovered `cursor/<exact-model-id>` entry.
4. Start with a short, non-sensitive text request. Then test one local tool with
   the normal PiG approvals enabled. `/usage` separately retrieves account meters.

No Cursor executable, extra API key or manual model list is required. Model
refresh uses the selected account's native credential, including the host's
serialized refresh-token rotation. A failed catalog request is an error, not an
empty successful catalog. Catalogs are cleared before refresh and are not saved
in a provider-global cache; offline startup or a failed refresh can therefore
show no models. Usage retrieval is independent of model selection and inference.

If an explicit `cursor-beta.json` exists at the usual agent-directory path, or
`PIG_CURSOR_BETA_CONFIG` selects one, the **existing CLI beta takes precedence**.
Move that file aside and unset the override to try the direct provider, then
restart. Do not delete your account credentials. Invalid beta configuration is
reported and does not silently activate the other transport. See
[CLI beta documentation](cursor-beta.md).

## Transports and tools

- Default: bidirectional HTTP/2 Connect protobuf `AgentService/Run`.
- For a proxy/network that cannot carry that stream, restart with
  `PIG_CURSOR_TRANSPORT=sse ./build/pig-plugins`. This explicitly selects
  `RunSSE` plus ordered unary `BidiAppend` requests **before** generation starts.
  Despite its name, RunSSE returns Connect frames, not OpenAI SSE JSON.
- No generation request is automatically retried or switched between transports,
  accounts or providers, even if it fails before text is visible. Retry manually
  only after checking for output and tool side effects.
- Local tools are advertised as MCP tools. The provider returns native tool-call
  proposals through OMP's external-client handoff; PiG performs its normal tool
  validation/approval/execution and sends the actual results on the next request.
  The provider itself never opens files or launches shells. Repeated identical
  proposals in one remote turn are deduplicated; conflicting duplicates fail.
- Cursor-native shell/filesystem/edit/other exec variants and remote interaction
  queries are **not implemented**: they fail closed instead of bypassing PiG.
  Models are instructed to use the advertised MCP tools. If the service insists
  on native exec, that request will fail; this is not full OMP feature parity.

## Limits and diagnostics

Text, incremental thinking/text output, role-separated history, tool/result
history, request-context rules and request-local SHA-256 blob exchange are
implemented. Images, signed reasoning replay, automatic effort normalization,
remote checkpoint resume, hosted web actions and Cursor-native tools are not.
Each continuation rebuilds history from PiG rather than retaining an opaque
account-global remote checkpoint. No remote generation or local tool is replayed
automatically after interruption.

Exact usable model slugs, including effort/speed variants, remain separate.
AvailableModels metadata supplies context limits when it can be matched. Otherwise
OMP's 200,000-context/64,000-output compatibility budgets are used (output is
clamped to context). **These defaults are estimates, not verified account/model
limits.** Output limits are metadata, not a guarantee that Cursor enforces a
requested maximum. No per-token price is invented. Select the exact discovered
variant; the generic thinking selector does not rewrite its wire ID. Unsupported
sampling or explicit reasoning-budget/effort settings are rejected.

Frames are capped at 8 MiB; lifetime stream and blob budgets at 64 MiB each;
individual immutable event snapshots at 64 MiB; lifetime work at 65,536 inbound
frames and 32,768 emitted events; queued client messages at 32; tools per remote
turn at 128; concurrent generations at four; requests at 30 minutes, unary
discovery at 15 seconds, and SSE appends at 30 seconds. Consumed snapshots do not
spend a cumulative snapshot-byte budget. Host/SDK patches `0011-cursor-provider-host.patch`
and `0012-cursor-provider-sdk.patch` apply
cancellable backpressure: one unread event per SDK/native Cursor queue, plus
bounded in-flight/terminal state. Slow consumers stall the producer, not grow
an unbounded queue. Consumers must drain events; waiting only for Result without
iteration can stall until cancellation/deadline. Ordinary providers retain their
existing result/iteration contract. Cancellation closes the stream and writer.
Excessive or malformed frames, missing turn/terminal status and errors after
partial text remain failures.
HTTP redirects are rejected. Credentials go only to fixed Cursor RPC origins;
caller model base URLs and caller headers cannot redirect authentication.
Remote error bodies/messages are withheld because they can contain secrets.

Response token usage is included only when the terminal turn reports it. Missing
values are unknown. Account `/usage` pools are separate from response tokens,
pricing and model entitlement; see [protocol review](cursor-direct-protocol.md).

## Verification and manual acceptance

Offline tests use local TLS HTTP/2 servers and synthetic credentials:

```sh
GOTOOLCHAIN=go1.27.2 go test -race ./internal/cursordirect ./extensions/cursor-login ./internal/cursorcli
GOTOOLCHAIN=go1.27.2 go vet ./internal/... ./extensions/... ./cmd/...
```

They cover catalog isolation, account switching/logout and superseded model
publication; native system/tool changes; fragmented streams and immutable events;
MCP handshake, duplicate proposals and tool-result continuation; explicit SSE
appends; cancellation, redirects, redaction, malformed/oversized wire data and
terminal failure without generation retry. A fused build is not live acceptance.

On your target, verify browser login, expired-token refresh, account switching,
model discovery, text, one approved tool/result/final answer, cancellation and
usage reconciliation. Start with a small usage budget and non-sensitive context.
For failures, retain the HTTP status or sanitized protocol error, model ID and
chosen transport. Do not share tokens, auth files, cookies or raw request bodies.
