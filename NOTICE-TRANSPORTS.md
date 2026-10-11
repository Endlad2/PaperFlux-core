# Transport provenance

PaperFlux is distributed under the GNU GPL version 3; see LICENSE.

- `transport/boards/boards.go` and IPv4-first dial logic are adapted from
  p1neappleXpress/OpenFlux v0.5.0, commit
  `7336e1b9d940fc09d00bbf545d15b9437487aa10` (GPL-3.0).
  Local changes include cancellation, bounded cleanup, URL validation and
  integration with PaperFlux's authenticated Session and verification IPC.
- `transport/mtslink` is a native Go implementation of the published wire
  protocol in trader52's MTS-Link script transport v1.0.0, from
  p1neappleXpress/OpenFluxTransports commit
  `5e24a8821a888e408c24e88ffaa373d35ee1f483`.
  It does not embed the JavaScript engine or copy the script package.
- Yandex cursor length and Mail.ru Firefox 155 WebSocket compatibility follow
  the upstream v0.4.3 corrections included in v0.5.0 (issues 165 and 163/164).

Sources:
https://github.com/p1neappleXpress/OpenFlux
https://github.com/p1neappleXpress/OpenFluxTransports
