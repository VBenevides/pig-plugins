# Cursor SDK Bridge prototype

This adapter uses the official [Cursor SDK Bridge v1.2.0](https://github.com/cursor/sdk-bridge/tree/v1.2.0).
It does not restore the removed private-protocol provider.
The published contract permits JSON Connect requests over local HTTP/1.1.
The implementation uses the Go standard library. It does not require a separate Python or Node installation.

## Optional installation

Run `sh scripts/install.sh --install-cursor` during distribution installation.
For an installed distribution, run `pig-plugins --install-cursor`.
Both paths install the pinned standalone bridge from Cursor's official release.
Normal installation and startup do not download the bridge.

The installer checks the pinned archive SHA-256 before extraction.
It uses a private staging directory and an exclusive lock.
It publishes a versioned installation only after verification.
Repeated installation checks local integrity without downloading again.
An invalid existing installation is preserved for manual diagnosis.

The installed path is `<agent-directory>/bin/cursor-sdk-bridge/1.2.0/cursor-sdk-bridge`.
Windows uses the `.exe` suffix. Unsupported platforms fail before download.
The upstream archive is downloaded directly to the user's machine, not bundled in pig-plugins.

## Browser sign-in

Run `/login` and select Cursor. Approve the request on Cursor's website.
The flow follows `Cursor.auth.login()` in `@cursor/sdk` 1.2.0: use the SDK
redirect, then exchange the browser session for a 90-day user API key through
`DashboardService.CreateUserApiKey`. The bridge receives that key, not the
browser session token. PiG stores the key in its native account store.
No browser refresh token is stored. When the key expires, sign in again.
Key creation is not retried after an uncertain response. If login fails after
key creation, review and revoke unused PiG keys in the Cursor dashboard.
Team policy can prevent user API key creation.

The [SDK authentication and billing documentation](https://cursor.com/docs/sdk/typescript.md#authentication)
states that user API keys bill to the user's plan and SDK runs use the same
request pools and pricing as IDE runs. This does not guarantee free or unlimited
requests. No paid request has been run for qualification.

## Current scope

The isolated adapter supports the bridge handshake, version checks, catalog calls,
agent creation, framed streaming, terminal-result checks, continuation, cancellation,
and local agent cleanup. Mock tests cover these operations.
The bridge runs in a private temporary workspace with one agent slot.
The adapter does not inherit Cursor, Node, proxy, or callback configuration from the parent process.

Bridge authentication and account authentication are separate.
Every RPC uses the local bearer token. Catalog and agent requests carry the account key explicitly.
The adapter accepts only literal loopback addresses. It does not use proxies, follow redirects, or retry RPCs.
Remote error text is excluded from diagnostics because it can contain credentials or prompts.

Agent creation disables built-in tools, inherited setting sources, MCP servers,
and sub-agent definitions. Tool proposals must return to PiG for validation and execution.
The security tests and native provider integration are separate release gates.
The prototype alone does not establish a working Cursor provider.

## Catalog persistence

The host model store saves the catalog with a hash of the account credential.
It stores no access or refresh token in that catalog. Offline refresh and restart
restore only the matching account's exact model and thinking selections. Logout
removes the stored catalog. An online failure preserves valid same-account data;
an account change cannot reuse the previous account's catalog.

The Accounts host patch registers dynamic SDK providers in the canonical model
collection, including providers whose initial static model list is empty.

## Verification limits

Browser sign-in and an authenticated catalog read now work with the user's account.
The live bridge returned 49 catalog entries, mapped to 47 selectable models.
The fused CLI and two fresh RPC sessions show the 47 models. These checks did
not send a generation request. Paid operations and tool execution remain unverified.
Offline results do not prove account access, model availability, billing accuracy,
or the effectiveness of upstream tool restrictions during real inference.
Do not claim production support until the required qualification gates pass.
Do not run paid checks without separate approval.

The bridge does not expose an upstream HTTP/2-to-SSE transport selector in the pinned public contract.
Local RPCs already use HTTP/1.1. Certificate verification must remain enabled.
A future transport adapter must use supported controls. It must not retry uncertain generation.

## Sources and distribution

- [Lifecycle and authentication](https://github.com/cursor/sdk-bridge/blob/v1.2.0/docs/protocol.md)
- [Service contract](https://github.com/cursor/sdk-bridge/tree/v1.2.0/proto/sdk/v1)
- [MIT license](https://github.com/cursor/sdk-bridge/blob/v1.2.0/LICENSE)
- [Release artifacts and checksums](https://github.com/cursor/sdk-bridge/releases/tag/v1.2.0)

The public repository is MIT-licensed. Preserve its notice when distributing its files.
The standalone archive includes additional runtime and SDK components.
Review the archive notices before changing redistribution policy.
