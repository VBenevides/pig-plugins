# Browser use

The native Go extension controls Chrome or Firefox through W3C WebDriver with a dedicated persistent profile.
It does not require Node, npm, OMP, or pi-browser-harness.
The selected browser must be installed. The extension downloads and starts its driver automatically.

## Automatic browser setup

1. Install Chrome/Chromium or Firefox, if it is not already available.
2. Start `pig-plugins`.
3. Select `/browser-use mode chrome` or `/browser-use mode firefox`.
4. Ask the agent to open a page.

For standalone use, load `pig -e ./extensions/browser-use` instead.
The mode command enables tools. The first browser tool checks the browser executable and version.
If the browser is missing, the extension reports an installation diagnostic before downloading a driver.
It checks PATH and common application locations. It does not download the browser itself.

The extension then resolves the latest compatible driver from official release metadata.
ChromeDriver must match the installed Chrome build. Automatic Chrome downloads use Chrome for Testing metadata for modern Chrome versions.
Firefox uses the latest published geckodriver. The browser must meet that release's Firefox requirements.
The extension validates the executable version before it starts the driver.
Geckodriver archives also require the SHA-256 digest from the official GitHub release metadata.
Chrome for Testing metadata supplies no archive checksum. Chrome downloads rely on official HTTPS metadata and assets.

Driver caches use these paths:

```text
~/.pig/agent/browser-use/drivers/chrome-<os>-<arch>/<version>/chromedriver
~/.pig/agent/browser-use/drivers/firefox-<os>-<arch>/<version>/geckodriver
```

Windows executable names include `.exe`. The base path follows `PIG_CODING_AGENT_DIR` or `PIG_HOME` when configured.
The extension reuses a cached driver when the resolved version is unchanged.
A new version must pass extraction, version checks, and driver startup before the extension deletes older version directories.
A failed download leaves the previous cached driver intact.
Only one version remains per browser/platform cache after a successful update.
Metadata checks require network access. The extension does not silently use stale metadata after a failed update check.

Each managed driver listens on a selected loopback port.
Off, browser changes, and session shutdown stop only the driver process owned by that client.
Headless/visible browser restarts reuse the same driver process and browser profile.
A cache lock prevents concurrent sessions from updating or using the same managed driver cache.
If a crash leaves a `.lock` directory, first verify that its old driver has stopped.
Only then remove that specific lock directory. Do not remove a lock held by another active session.

## External driver override

Set `PIG_BROWSER_CHROME_DRIVER_URL` or `PIG_BROWSER_FIREFOX_DRIVER_URL` before startup to use an existing driver.
This disables managed browser detection, driver download, and process startup for that browser.
The external driver controls browser availability. The extension never stops the external driver process.
1. Start the driver in a separate terminal:

   ```sh
   chromedriver --port=9515 --allowed-ips=127.0.0.1
   ```

2. Set the endpoint in the terminal where you start PiG:

   ```sh
   export PIG_BROWSER_CHROME_DRIVER_URL=http://127.0.0.1:9515
   ```

For Firefox, use `geckodriver --host 127.0.0.1 --port 4444 --log error` and the matching Firefox environment variable.
Only HTTP URLs with literal loopback addresses and explicit ports are accepted.
Credentials, remote hosts, DNS names, redirects, proxies, and custom endpoint paths are not supported.

## Mode and context

Mode stays in memory only. Every session start resets it to off, including `/new`, session replacement, reload, and process restart.
Browser profiles persist independently of this mode. Selecting off or starting a new session does not delete site logins.
Repeated selection of the same mode preserves the automation session.
Changing browsers or selecting off cancels current browser requests and deletes only the extension-owned WebDriver session.
A failed cleanup produces a warning. Check the driver for a remaining automation browser after that warning.
The extension stops its own driver process. It never stops external drivers or attaches to personal browser profiles.

Off mode gives the tools hidden exposure and removes them from the active tool list.
The model receives no browser tool declarations, prompt snippets, or browser guidelines from this extension.
Tool discovery cannot find them while off.
Existing user messages and earlier browser results remain in conversation history.
Disabling a tool does not erase that history.

Enabled mode adds a `Browser: chrome` or `Browser: firefox` badge beside Mode and ADHD in the bundled footer.
Off mode removes the badge. The normal host status area shows the badge in standalone mode.

## Visibility

Visibility controls the browser window, not tool exposure. The default is `hidden`.

```text
/browser-use visibility hidden
/browser-use visibility login
/browser-use visibility always
```

| Visibility | Behavior |
| --- | --- |
| `hidden` | Start headless. Never open a visible browser. If login is required, use another authorized method or public source. |
| `login` | Start headless. Restart visibly for direct login. After confirmation, restart headless with the same profile. |
| `always` | Start visibly and keep the browser visible. |

Visibility stays in memory. Every session start resets it to `hidden`.
Selecting off retains visibility for the current session. It does not delete the profile.
A visibility change can restart the automation browser. The restart restores only its current HTTP or HTTPS URL.
It closes other tabs and discards unsaved form state. It invalidates old element references.
The extension does not repeat clicks, form submissions, or other writes after a restart.

In `login`, the dialog offers **Logged in** and **Other**.
After **Logged in**, the extension checks the page again after the headless restart.
If the login does not persist, the extension reports an error. It does not repeat the visible login automatically.
Select `always` explicitly if the site requires a visible browser, or use another authorized method.
**Other**, cancellation, and login errors close the visible login browser without revisiting the unconfirmed page.
The next permitted operation starts headless with the same profile.

## Persistent profiles and manual login

The extension always uses one dedicated profile per browser:

- Chrome: `~/.pig/agent/browser-use/chrome`
- Firefox: `~/.pig/agent/browser-use/firefox`

The paths follow `PIG_CODING_AGENT_DIR` or `PIG_HOME` when configured.
The extension creates profile directories with owner-only permissions. It never copies your everyday browser profile.
Cookies and browser storage remain on disk after off, `/new`, process exit, or browser restart.
Website expiration and logout rules still apply. Some sites require a new login after their session cookies expire.
Only one automation browser can use a profile at a time. Close the existing automation browser before another session uses it.
Do not delete browser lock files to force concurrent profile access.

Managed drivers remove inherited `MOZ_HEADLESS` so the visibility setting controls Firefox.
For external Firefox drivers in `login` or `always`, start geckodriver without `MOZ_HEADLESS`.
If the driver forces headless mode, the extension reports an error instead of showing a login dialog.
These modes require a desktop display. Hidden mode does not require a display.
The driver must share the host filesystem and profile path.

1. Enable the selected browser mode.
2. Select `/browser-use visibility login` or `/browser-use visibility always`.
3. Ask the agent to open the secured site.
4. If the login dialog appears, enter credentials and verification codes directly in the browser.
5. Select **Logged in** after the site completes login.

The dialog offers **Logged in** and **Other**. It times out after ten minutes.
**Other**, cancellation, and timeout leave login unconfirmed. The agent must ask how to proceed before using the secured site.
After **Logged in**, the extension reads a new snapshot and rejects confirmation if the page still shows a detected login form.
Other browser tools remain paused while the dialog is open.

In `login` and `always`, detected login forms trigger the dialog after navigation, clicks, tab changes, or snapshots.
Visible password fields and some common username/login routes identify these forms.
In `hidden`, the extension returns alternate-method guidance instead. It never opens a visible browser or requests credentials.
Detection is a heuristic. It does not identify every SSO, iframe, passkey, or verification flow.
In `login` and `always`, ask the agent to use `browser_login` for login flows that detection misses.
The extension never asks for passwords in chat. `browser_fill` rejects password and file inputs.
Login confirmation records your response, not independent proof that the site granted the requested permissions.

Protect these profiles as sensitive account data. Do not commit, publish, or share them.
The extension does not export cookies to the model. Authenticated page text and images can still reach the model provider.

## Tools

| Tool | Input | Purpose |
| --- | --- | --- |
| `browser_navigate` | `url` | Navigate to an HTTP or HTTPS page. |
| `browser_snapshot` | None | Read page text and interactive element references. |
| `browser_login` | None | Request direct login in `login` or `always`. In `hidden`, return alternate-method guidance. |
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
Optional upstream selectors, click coordinates, screenshot formats, full-page captures, personal-profile attachment, and custom scripts are not supported.
The existing web-search tools remain unchanged.

## Access controls

Use a trusted driver on a trusted local machine. WebDriver grants browser control without a PiG authentication layer.
Do not expose the driver port to other machines. Other local users can reach a loopback TCP service.
The extension adds no daemon or shared Unix socket. It does not provide a multi-user driver security boundary.

Selecting a browser mode authorizes navigation, clicks, field changes, and tab switches without per-action browser confirmation.
These actions also work without a confirmation UI. Manual login still requires an interactive UI and explicit login confirmation.
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
Live Firefox checks passed headless navigation, input events, clicks, snapshots, screenshots, tab selection, and login detection.
A live profile check verified headless-to-visible-to-headless transitions and authenticated cookie and localStorage reuse after restart.
Automated host dialog tests cover **Logged in**, **Other**, cancellation, and persistent login-form detection in `login` and `always`.
Host tests also verify hidden-mode guidance and visibility reset after a new session.
No real-account credentials or manual human login were used during these checks.
Chrome runtime behavior remains unverified where Chrome and ChromeDriver are unavailable.
A live managed Firefox check passed automatic download, startup, cache reuse, and owned-driver shutdown.
Set `PIG_BROWSER_LIVE_MANAGED=1` to repeat that check with an isolated cache.
External-driver live checks use `PIG_BROWSER_LIVE_CHROME_DRIVER_URL` and `PIG_BROWSER_LIVE_FIREFOX_DRIVER_URL`.

## OMP compatibility boundary

The source comparison uses OMP revision `a483884da868db2c4dd21463dc03899ba64be6ff`.
OMP exposes one `browser` tool with `open`, `close`, and `run` actions.
Its `run` action uses an OMP JavaScript runtime and worker protocol.
This extension exposes fixed WebDriver operations instead. OMP scripts and tab objects are not interchangeable with these tools.

| Connection | OMP | This extension |
| --- | --- | --- |
| Automation browser | Shared Chromium with Puppeteer | Chrome or Firefox with a dedicated persistent WebDriver profile |
| Existing CDP endpoint | Supported through `app.cdp_url` | Not supported |
| Existing Chrome tabs | MV3 extension and OMP relay | Not supported |
| Browser process | Separate connected, spawned, and shared ownership rules | Driver owns the automation process, and the dedicated profile persists |
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
- [Chrome headless mode](https://developer.chrome.com/docs/automation-and-testing/headless)
- [Firefox headless arguments](https://developer.mozilla.org/en-US/docs/Web/WebDriver/Reference/Capabilities/firefoxOptions)
- [ChromeDriver custom profiles](https://developer.chrome.com/docs/chromedriver/capabilities)
- [geckodriver in-place profiles](https://firefox-source-docs.mozilla.org/testing/geckodriver/Profiles.html)
- [pi-browser-harness source comparison](https://github.com/amankumarsingh77/pi-browser-harness/tree/0c68d0c74eeac7838f4a5f4fab38a76e078e72d7/src/domains)
- [OMP browser schema and dispatch](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/coding-agent/src/tools/browser.ts)
- [OMP relay wire protocol](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/coding-agent/src/tools/browser/relay/protocol.ts)
- [OMP relay ownership and security limits](https://github.com/can1357/oh-my-pi/blob/a483884da868db2c4dd21463dc03899ba64be6ff/packages/browser-relay/README.md)

This implementation uses the WebDriver protocol directly. It does not copy or bundle upstream browser-harness code.
