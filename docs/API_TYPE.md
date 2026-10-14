# `--api_type`: raw vs browser

PaperFlux has two ways to reach a document provider. They are orthogonal to
`--transport`: the transport picks the provider, `--api_type` picks who holds
the WebSocket.

| `--api_type` | Who speaks to the provider | When to use |
| --- | --- | --- |
| `raw` (default) | Go, using the existing HTTP + WebSocket code | Stable networks, no CAPTCHA, servers without a browser |
| `browser` | A real browser (Playwright on desktop, WebView on mobile) | CAPTCHA-heavy documents, frequent 429, expiring cookies |

## What changes

Only the carrier. The bytes inside the cursor are identical:

```

PFMB batch -> PFS2/Session encryption -> base64 -> cursor string

```

Session, batching, encryption, `--urls`, `--volga-url`, `--profile-id` and
`--profile-token` all behave exactly as before.

## What the browser backend does

- Opens one tab per document URL.
- Lets the provider's own page pass CAPTCHA and hold cookies.
- Adopts the editor's WebSocket by patching `window.WebSocket`.
- Sends cursor frames via `window.__paperflux.sendCursor(b64)`.
- Receives cursor frames via `window.__paperfluxInbound(text)`.

Nothing is written to the document, and no editor command other than
`cursor` is emitted. Coauthoring, history and permissions are untouched.

## Desktop

Playwright downloads a driver (~50 MB) and a Chromium build on first start.
Set `DesktopHeadless = true` in `browser/desktop/playwright.go` for
production; it ships as `false` so a developer can watch the tab.

```bash
./paperflux --client --api_type=browser --transport yandex \
    --url "https://disk.yandex.ru/i/DOC_ID" \
    --profile-id 1 --profile-token "KEY"
```

## Mobile

The host application owns the WebView. Go talks to it over the IPC socket
with four new message types; see `browser/README.md` for the Kotlin/Swift
integration sketches.

Pass `--api_type=browser` together with `--ipc-socket` so Go knows where the
host listens:

```
./paperflux --client --api_type=browser --transport yandex \
    --url "https://disk.yandex.ru/i/DOC_ID" \
    --profile-id 1 --profile-token "KEY" \
    --ipc-socket /data/local/tmp/paperflux.sock
```

## Known limitations

- One browser context per process, so profiles share cookies.
- Yandex editor socket rotation (4007) is not yet re-hooked automatically.
- Mobile performance depends on the host WebView; the Android WebView is
noticeably slower than Playwright's Chromium.

