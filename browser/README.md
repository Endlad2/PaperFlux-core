# browser — real-browser transport backend

This package implements `--api_type=browser`: instead of speaking to Yandex
Docs / Mail.ru Docs directly over HTTP + WebSocket, PaperFlux drives a real
browser that already has an authenticated editor session, and pushes cursor
frames through the editor's own socket.

## Why

The raw transports fight the provider's anti-bot layer:

- `showcaptchafast` / SmartCaptcha,
- HTTP 429 with `Retry-After` up to 15 minutes,
- Mail.ru `ACCESS_DENIED` / `4004` on cursor length mismatch,
- cookies that expire and have to be re-injected by hand.

A real browser wins all of those for free: it solves CAPTCHA, holds
session cookies, and speaks the exact protocol the provider expects.

## What does NOT change

The browser backend is orthogonal to the transport list. `--transport`
still selects the provider (`yandex`, `mailru`, ...), and the wire format
inside the cursor stays identical:

```

PFMB batch  ->  PFS2 or Session encryption  ->  base64  ->  cursor string

```

`--api_type` only chooses *who holds the WebSocket*: Go, or the browser.

## Persistent profile

The desktop backend launches Chromium with `LaunchPersistentContext`, so
Chromium writes its whole profile to disk and reads it back on the next
run. Cookies, localStorage, IndexedDB and the HTTP cache all survive a
PaperFlux restart.

Default profile location:

| OS | Path |
| --- | --- |
| Windows | `%LOCALAPPDATA%\PaperFlux\browser-profile` |
| macOS | `~/Library/Application Support/PaperFlux/browser-profile` |
| Linux | `~/.local/share/paperflux/browser-profile` (or `$XDG_DATA_HOME`) |

The directory is created with mode `0700` on first launch. Delete it to
force a fresh login and a fresh CAPTCHA.

`Config.ProfileDir` overrides the location. The browser prints the path it
uses at startup (`[BROWSER] launching persistent Chromium ... profile=...`).

### First run vs subsequent runs

- **First run**: the profile is empty. The provider may show a CAPTCHA or a
  login page. Solve it once in the visible Chromium window.
- **Subsequent runs**: Chromium loads the saved cookies and the editor
  opens directly. No CAPTCHA, no login.

If the provider invalidates the session (logout, password change, IP
change), delete the profile directory and repeat the first run.

## Desktop

`browser/desktop` uses [playwright-go](https://github.com/mxschmitt/playwright-go).

- `Install()` runs `playwright.Install()` (downloads driver + browsers).
- `Launch()` starts a persistent Chromium rooted at `ProfileDir`.
- `OpenTab(url)` navigates, waits for the editor, and installs the hook.
- `Evaluate(tab, script)` runs JS in the page.
- `Inbound(tab)` streams cursor frames back to Go.

`DesktopHeadless` is `false` while the backend is being tested so a human
can watch the tab. Flip it to `true` before shipping.

### Editor URLs

A shared Yandex link (`https://disk.yandex.ru/i/<id>`) is a preview page
with no editor WebSocket. The editor lives at
`https://docs.yandex.ru/edit/d/<id>`. The provider redirects from the
shared link to the editor URL in-page, but headless Chromium can stall
before that redirect.

`OpenTab` handles both cases:

1. It waits for the in-page redirect, re-installing the hook on every
   `framenavigated`.
2. If the page is still on the preview host after the first poll, it
   navigates directly to the editor URL derived from the shared link.

The `sk=` query parameter that the provider adds to the editor URL is not
synthesized: it is issued by the provider after a real browser session and
is present once the persistent profile holds a valid login.

## Mobile

`browser/mobile` does not implement a WebView. Android (Kotlin/Java) and
iOS (Swift) own the WebView; Go talks to it over the existing IPC socket
using four new message types defined in `transport/ipc/browser_messages.go`.

Mobile has no persistent profile concept on the Go side: the host WebView
owns its own storage (Android `WebView.cookieManager`, iOS `WKWebsiteDataStore`).
