# Direct Cursor provider: protocol review

## Status and provenance

Reference implementation: [can1357/oh-my-pi](https://github.com/can1357/oh-my-pi/tree/3a335e71c3c9382a92be209f90b8b52a1f7c640d), revision `3a335e71c3c9382a92be209f90b8b52a1f7c640d`.
The repository license is MIT, copyright Mario Zechner (2025), Can Bölük
(2025–2026), and Stencil Labs, Inc. (2026). Retain its license when copying
substantial code or schemas. This document describes source inspection, not a
successful account-backed compatibility test.

An experimental direct provider is now implemented in `internal/cursordirect/`
and registered by default when no explicit CLI-beta configuration is selected.
See [setup and offline verification](cursor-direct.md). Existing `cursor-beta.json`
configurations continue to select the CLI adapter. No partially generated request
is retried using a different transport, account, or provider.

## Authentication and origins

OMP's `packages/ai/src/registry/oauth/cursor.ts` uses browser PKCE at
`https://cursor.com/loginDeepControl`, polling
`https://api2.cursor.sh/auth/poll`, and refresh-token exchange at
`https://api2.cursor.sh/oauth/token`. Our existing Cursor login follows this
flow and retains the `cursor` provider identity. Credentials belong in the
native account store, not a second provider-specific credential file.

Inference/catalog requests use the selected access token as a bearer. Dashboard
usage is a separate authentication surface: OMP derives the user ID from the
JWT subject and sends the URL-encoded `userId::accessToken` value as a
`WorkosCursorSessionToken` cookie to `https://cursor.com/api/usage-summary`.
Do not send that cookie to the inference origin or infer authorization from an
unverified JWT payload. The server must authenticate the token.

All authenticated requests must use fixed required HTTPS origins, reject
redirects, preserve TLS verification, bound reads/timeouts, and redact response
bodies and transport errors that could contain credentials. Account switching
must invalidate account-scoped catalogs, usage and conversation state. Refresh
must preserve rotated tokens and cannot overwrite a newer account credential.

## Discovery

See OMP `packages/catalog/src/discovery/cursor.ts`,
`cursor-models.proto`, and `packages/catalog/src/wire/cursor.ts`.

- `/agent.v1.AgentService/GetUsableModels`: exact account-usable wire slugs.
- `/aiserver.v1.AiService/AvailableModels`: structured capability/variant data.
- `/agent.v1.AgentService/GetDefaultModelForCli`: default-model selection.

These are protobuf/Connect RPCs, not OpenAI `/v1/models`. OMP sends bearer auth,
Connect protocol version and Cursor client-type/version headers. The reviewed
wire module identifies client build `cli-2026.09.02-c22c1a3`; that is upstream
compatibility metadata, not evidence that pig-plugins is an official client.
A successful empty catalog differs from a transport/decode failure. Unknown
context/output limits, prices and capabilities must remain unknown. Preserve
exact raw IDs if effort/speed variants are collapsed for display.

## Streaming and tools

See OMP `packages/ai/src/providers/cursor.ts` and
`packages/ai/src/providers/cursor/proto/agent.proto`.

- HTTP/2 `/agent.v1.AgentService/Run` carries bidirectional Connect protobuf
  frames, heartbeats, agent requests and client replies.
- HTTP/1 fallback uses `/agent.v1.AgentService/RunSSE` and
  `/aiserver.v1.BidiService/BidiAppend`. This is not interchangeable with a
  chat-completions SSE endpoint. Retry/fallback requires proof that generation
  has not started; network failure alone is insufficient.
- Requests contain conversation state, user actions, model routing and MCP
  definitions. Continuation also uses checkpoint/blob state. Flattening the
  transcript into a single prompt is not an equivalent implementation.
- Interaction updates carry text, thinking, partial tool arguments and turn
  completion. A turn-ended update alone is not transport success; validate the
  terminal Connect/gRPC status and reject truncation.
- Exec messages include filesystem, shell, MCP, request-context and other
  operations. They must not execute outside PiG's approved tool loop. Unknown
  operations must fail closed. Tool IDs/results and continuation state must
  prevent duplicate effects after interruption.

The pinned SDK has native `Provider.GetModels`, `FilterModels`,
`RefreshModels`, `ProviderStreamOptions.Signal` and `RefreshModelsContext`
(including credential, network permission and publication callbacks). Prefer
these lifecycle APIs over reading account files or retaining one global token.
The existing map-based `OAuthProvider` login callbacks alone do not implement
credential-aware dynamic model discovery or the agent protocol.

## Usage

See OMP `packages/ai/src/usage/cursor.ts` at the pinned revision.

`https://api2.cursor.sh/auth/usage` is the legacy bearer-auth request-count API.
`https://cursor.com/api/usage-summary` reports personal/dashboard usage. OMP
handles `individualUsage.overall`, `plan` and `onDemand`, USD-cent amounts,
explicit reset dates, and separate `autoPercentUsed`/`apiPercentUsed` rails.
Do not treat the legacy always-zero uncapped counter as unlimited quota.

Cursor Models and Other Models are distinct pools. Exhaustion of one must not
block an unrelated pool. Usage retrieval/display must not change authentication
or model routing. Missing data is unknown, not zero; subscription balance is
not per-response token usage or a guaranteed inference entitlement.

## Service restrictions and acceptance gate

Cursor's [API overview](https://cursor.com/docs/api) says its supported APIs
and SDKs run agent workflows, not standalone inference/chat completions. Its
[terms](https://cursor.com/terms-of-service) and
[acceptable-use policy](https://cursor.com/acceptable-use-policy) include
reverse-engineering, scraping and access-control restrictions, subject to their
stated qualifications. OMP's MIT license does not grant Cursor service rights.
This review is not a legal determination or a claim of official support.
Do not bypass restrictions, spoof entitlement, or probe accounts without consent.

The user explicitly authorized offline implementation from the pinned OMP source
and will perform live acceptance later. Local fake-server tests now exercise the
implemented catalog, Connect streams, blob/history conversion, MCP tool handoff
and native model publication. They do not establish current server acceptance,
account entitlement, supported plan behavior or end-to-end PiG approval behavior
against a live Cursor service. No real credentials or authenticated requests
were used during implementation. Live acceptance is a release/compatibility gate,
not a prerequisite for writing or testing the offline implementation.

## Required verification

1. Offline fixtures: malformed/fragmented/oversized frames, empty/denied catalogs,
   account changes, bounded refresh, redirects, secret-bearing errors, unknown
   exec messages, cancellation, terminal failure after output and no replay.
2. Fused binary with no Cursor executable: native login/catalog/usage lifecycle,
   direct streaming and a deterministic approved tool/result/final-answer flow.
3. Separately authorized target: login/refresh, bounded discovery/text probe,
   usage reconciliation, cancellation and an approved tool round trip. Record
   actual results and any unsupported plans without logging tokens or prompts.

No live checks in this list have been performed. Existing offline CLI-beta tests
are not evidence that this direct protocol works.
