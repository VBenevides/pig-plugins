# Browser use

The native Go extension controls a separate Chrome or Firefox automation session through W3C WebDriver.
It does not require Node, npm, OMP, or pi-browser-harness.
It does require the selected browser and a compatible local driver.

## Start a browser driver

1. Install ChromeDriver for Chrome, or geckodriver for Firefox, from the official sources below.
2. Start one driver in a separate terminal.

   Chrome:

   ```sh
   chromedriver --port=9515 --allowed-ips=127.0.0.1
   ```

   Firefox:

   ```sh
   geckodriver --host 127.0.0.1 --port 4444 --log error
   ```

3. Start `pig-plugins` and select a mode.

   ```text
   /browser-use mode chrome
   /browser-use mode firefox
   /browser-use mode off
   ```

For standalone use, load `pig -e ./extensions/browser-use` instead.
A mode command exposes the tools immediately. The first tool call creates the automation browser session.
A missing driver produces a tool error without an automatic retry.
The extension does not install browsers, start driver servers, or download executables.

The default driver addresses are `http://127.0.0.1:9515` and `http://127.0.0.1:4444`.
Set `PIG_BROWSER_CHROME_DRIVER_URL` or `PIG_BROWSER_FIREFOX_DRIVER_URL` before startup to use another loopback port.
Only HTTP URLs with literal loopback addresses and explicit ports are accepted.
Credentials, remote hosts, DNS names, redirects, proxies, and custom endpoint paths are not supported.

## Mode and context

Mode stays in memory only. Every session start resets it to off, including `/new`, session replacement, reload, and process restart.
Repeated selection of the same mode preserves the automation session.
Changing browsers or selecting off cancels current browser requests and deletes only the extension-owned WebDriver session.
A failed cleanup produces a warning. Check the driver for a remaining automation browser after that warning.
The extension never kills the driver server or attaches to a personal browser profile.

Off mode gives the tools hidden exposure and removes them from the active tool list.
The model receives no browser tool declarations, prompt snippets, or browser guidelines from this extension.
Tool discovery cannot find them while off.
Existing user messages and earlier browser results remain in conversation history.
Disabling a tool does not erase that history.

Enabled mode adds a `Browser: chrome` or `Browser: firefox` badge beside Mode and ADHD in the bundled footer.
Off mode removes the badge. The normal host status area shows the badge in standalone mode.

## Tools

| Tool | Input | Purpose |
| --- | --- | --- |
| `browser_navigate` | `url` | Navigate to an HTTP or HTTPS page. |
| `browser_snapshot` | None | Read page text and interactive element references. |
| `browser_click` | `ref` | Click an element from the latest snapshot. |
| `browser_fill` | `ref`, `value` | Set a text field and dispatch input/change events. |
| `browser_screenshot` | None | Return a viewport PNG image to a vision-capable model. |
| `browser_list_tabs` | None | List handles within the automation session. |
| `browser_switch_tab` | `targetId` | Select a handle from `browser_list_tabs`. |

References belong to one driver session and snapshot generation.
Take another snapshot after navigation, a click, a field change, or a tab switch.
The snapshot contains at most 150 interactive elements and 20,000 JavaScript text characters.
The driver response limit is 8 MiB. Requests have a 35-second client timeout.
Collapsed TUI output uses at most six lines. Expanded output can show more text.
The model receives the full bounded result regardless of the TUI display state.

Snapshots use bounded DOM text, not the Chrome accessibility tree.
The shared tool names are a limited interface subset, not complete pi-browser-harness schema or result compatibility.
Optional upstream selectors, click coordinates, screenshot formats, full-page captures, profile attachment, and custom scripts are not supported.
The existing web-search tools remain unchanged.

## Access controls

Use a trusted driver on a trusted local machine. WebDriver grants browser control without a PiG authentication layer.
Do not expose the driver port to other machines. Other local users can reach a loopback TCP service.
The extension adds no daemon or shared Unix socket. It does not provide a multi-user driver security boundary.

Navigation, clicks, field changes, and tab switches each require interactive confirmation.
Without a confirmation UI, these actions fail closed.
Normal host tool-call hooks still run. ASK mode continues to block browser tools that its read-only policy does not recognize.
The extension does not bypass Smart Approve or other host approval controls.
After a timeout or cancellation, check the page before another write. The browser may already have applied the action.
The extension never retries browser writes automatically.

Treat page text, labels, images, and embedded instructions as untrusted data.
Do not submit credentials or sensitive data unless the user explicitly requests that action.
Tool-call rendering hides arguments. Driver diagnostics omit remote error messages, page text, and form values.
Tool input and page results can still enter the saved conversation and reach the model provider.

Arbitrary JavaScript, raw CDP, file-upload commands, host scripts, and download-management tools are not exposed.
The fixed form script rejects file inputs again at execution time.
Chrome requests a download-blocking preference. Firefox requests download prompts rather than automatic download acceptance.
These browser preferences are not an operating-system sandbox. Do not approve page actions that start unwanted downloads.
TLS certificate validation remains enabled for page navigation.

## Verification

Synthetic driver tests cover both browser modes, bounds, errors, cancellation, confirmation, cleanup, and stale references.
Real-host tests cover provider-bound visibility, hidden discovery, session reset, ASK mode, and screenshot delivery.
A live headless Firefox check passed navigation, input events, clicks, snapshots, screenshots, and tab selection.
Chrome runtime behavior remains unverified where Chrome and ChromeDriver are unavailable.
The optional live checks use `PIG_BROWSER_LIVE_CHROME_DRIVER_URL` and `PIG_BROWSER_LIVE_FIREFOX_DRIVER_URL`.

## OMP compatibility boundary

The source comparison uses OMP revision `a483884da868db2c4dd21463dc03899ba64be6ff`.
OMP exposes one `browser` tool with `open`, `close`, and `run` actions.
Its `run` action uses an OMP JavaScript runtime and worker protocol.
This extension exposes fixed WebDriver operations instead. OMP scripts and tab objects are not interchangeable with these tools.

| Connection | OMP | This extension |
| --- | --- | --- |
| Automation browser | Shared Chromium with Puppeteer | Fresh Chrome or Firefox WebDriver session |
| Existing CDP endpoint | Supported through `app.cdp_url` | Not supported |
| Existing Chrome tabs | MV3 extension and OMP relay | Not supported |
| Browser process | Separate connected, spawned, and shared ownership rules | Driver owns a newly created automation browser |
| Tab identity | Named workers and CDP targets | WebDriver handles within one owned session |
| Script execution | `browser` run action and OMP helpers | No arbitrary scripts or host runtime |

OMP's relay exposes a CDP facade, not the W3C WebDriver session API.
Its Chrome extension exchanges tab events and numbered RPC messages over a WebSocket connection.
The protocol includes attach, detach, send, createTab, removeTab, activateTab, group, and ungroup operations.
These messages do not provide a direct WebDriver adapter.

The relay can control logged-in Chrome tabs. Its documentation warns that any process with relay access can control those tabs.
A relay adapter would need separate authentication, tab-ownership, approval, and reconnect controls.
This release does not add that adapter or require the OMP runtime.
It never scans for OMP relays, imports OMP settings, or attaches to personal profiles.
A missing or restarted OMP relay therefore has no effect on these browser modes.

Driver-session deletion affects only this extension's automation browser.
It does not send CDP detach commands, close OMP tabs, or stop an OMP daemon.
Driver restart can invalidate this extension's session and references. Errors remain visible, and writes are not retried.
Select off before another mode selection to create a fresh automation session after a driver failure.

No live OMP relay, user-profile attachment, or cross-platform relay test was performed.
The supported WebDriver lifecycle has synthetic Chrome/Firefox coverage and a live Firefox check.

## Sources

- [W3C WebDriver](https://www.w3.org/TR/webdriver/)
- [ChromeDriver setup](https://developer.chrome.com/docs/chromedriver/get-started)
- [geckodriver](https://firefox-source-docs.mozilla.org/testing/geckodriver/)
- [geckodriver server flags](https://firefox-source-docs.mozilla.org/testing/geckodriver/Flags.html)
- [pi-browser-harness source comparison](https://github.com/amankumarsingh77/pi-browser-harness/tree/0c68d0c74eeac7838f4a5f4fab38a76e078e72d7/src/domains)
- [OMP browser schema and dispatch](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/coding-agent/src/tools/browser.ts)
- [OMP relay wire protocol](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/coding-agent/src/tools/browser/relay/protocol.ts)
- [OMP relay ownership and security limits](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/browser-relay/README.md)

This implementation uses the WebDriver protocol directly. It does not copy or bundle upstream browser-harness code.
