# Experimental board transports

Both ends require a PaperFlux build containing the corresponding transport.
Updating only Android does not add a new carrier to an existing exit server.
No automatic migration of users' document profiles is performed.

## Yandex Boards

Provider key: `boards`. Use one separate empty board with guest editing enabled.
Supported public URLs are `https://boards.yandex.ru/whiteboard/?hash=...` and
`https://boards.yandex.ru/guest/?hash=...`. Server and client use the same URL.
Never reuse a board containing user work: this carrier temporarily creates
small packet objects and deletes only objects matching its wire format.

The wire implementation is adapted from upstream OpenFlux v0.5.0. It uses
`modify-objects`, not the obsolete cursor channel. Engine.IO 4 server pings
are answered with pong; the client does not send reverse-direction pings.
Each reconnect obtains fresh guest authorization. Stop cancels authentication,
closes the socket and joins its bounded workers.

Boards sessions use upstream v0.5.0's CUBIC, 4/16/64 MiB TCP buffer ranges,
classic loss recovery and a three-second minimum RTO on both ends. These
parameters are per profile, including inside a mixed-provider lazy exit group.
Mail.ru/Docs defaults are not altered. `PAPERFLUX_BOARDS_TCP_PRESET=legacy`
retains the previous Boards settings for bounded A/B tests. Queue memory and
flow limits still apply; larger TCP windows do not disable these safeguards.
Boards batch retries use 10 ms intervals but retain the ten-second expiration
and cancellation bounds, unlike upstream's unbounded pending retry loop.
The batch wire format and normal 8 KiB batching size remain unchanged.

## MTS Link Boards

Provider key: `mtslink`. Use one public board link on `boards.mts-link.ru` or
`my.mts-link.ru`, with guest participation permitted. This native implementation
follows the official OpenFluxTransports MTS-Link v1.0.0 wire protocol. It does
not require importing the JavaScript runtime. `initResponse` must confirm a
session before data can be sent. Ping and read timeouts detect silent stalls.

MTS was verified only against local protocol fixtures, not a live account or
board. It remains experimental until tested with a real owned guest board.

## Profiles

Existing JSON / `paperflux://config` / QR / encrypted bootstrap formats are
unchanged. Set `transport` to `boards` or `mtslink` and supply exactly one board
as `documentUrl` (or as the sole entry of `documentUrls`). Keep the server's
real profile ID, access token and virtual IP. Do not use SSH passwords as
profile access tokens. The two peers must have the same profile identity.

Core profile catalogs and exit manifests recognize both providers. Availability
in the Telegram bot is configured separately. Importing a complete profile
does not automatically provision its board on an existing server.
