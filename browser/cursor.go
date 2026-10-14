package browser

import "fmt"

// cursorScript installs the PaperFlux cursor bridge inside an authenticated
// document tab. It works in every frame, not just the top one: Mail.ru serves
// its editor from docs.datacloudmail.ru inside an iframe, and a cross-origin
// iframe cannot call a function on the parent directly.
//
// Protocol:
//   - Every frame patches window.WebSocket and adopts the editor's socket.
//   - When a cursor frame arrives (or is sent), the frame forwards it:
//       * to window.__paperfluxInbound() if present (same-origin, top frame);
//       * otherwise via window.parent.postMessage({__paperflux: text}, "*").
//   - The top frame listens for window.postMessage and forwards anything with
//     the __paperflux marker to window.__paperfluxInbound.
//
// Sending still happens in the top frame: sendCursor() writes to whichever
// socket the top frame adopted. If the editor lives in an iframe, the top
// frame asks the iframe to send via postMessage (type=send).
//
// The script is idempotent per frame.
const cursorScript = `
(() => {
  // ---- top frame: receiver + sender facade ----
  if (window.top === window) {
    if (window.__paperflux && window.__paperflux.ready) return "ready";

    // Adopt any WebSocket created by this frame's own code (the top frame may
    // itself host a socket for some editor layouts).
    const TopWS = window.WebSocket;
    const state = { socket: null, adopted: 0, childFrames: [] };

    function isCursorFrame(text) {
      if (typeof text !== "string") return false;
      return text.indexOf('"type":"cursor"') !== -1 && text.indexOf('"cursor"') !== -1;
    }

    function deliver(text) {
      if (!isCursorFrame(text)) return;
      try {
        if (typeof window.__paperfluxInbound === "function") {
          window.__paperfluxInbound(text);
        }
      } catch (e) {}
    }

    function PaperFluxWS(url, protocols) {
      const ws = protocols === undefined ? new TopWS(url) : new TopWS(url, protocols);
      state.socket = ws;
      state.adopted++;
      ws.addEventListener("message", (ev) => deliver(ev.data));
      return ws;
    }
    PaperFluxWS.prototype = TopWS.prototype;
    PaperFluxWS.CONNECTING = TopWS.CONNECTING;
    PaperFluxWS.OPEN = TopWS.OPEN;
    PaperFluxWS.CLOSING = TopWS.CLOSING;
    PaperFluxWS.CLOSED = TopWS.CLOSED;
    window.WebSocket = PaperFluxWS;

    // Receive frames forwarded by child iframes.
    window.addEventListener("message", (ev) => {
      if (!ev || !ev.data || typeof ev.data !== "object") return;
      if (ev.data.__paperflux === "cursor" && typeof ev.data.text === "string") {
        deliver(ev.data.text);
      }
    });

    window.__paperflux = {
      ready: false,
      sendCursor(b64) {
        const line = JSON.stringify(["message", { type: "cursor", cursor: b64 }]);
        const msg = "42" + line;

        // Prefer the top frame's own socket when it is live.
        if (state.socket && state.socket.readyState === 1) {
          state.socket.send(msg);
          return "sent";
        }
        // Otherwise ask every child frame to try. First "sent" wins; the child
        // that carries the editor socket responds and the rest are no-ops.
        let dispatched = 0;
        for (const f of document.querySelectorAll("iframe")) {
          try {
            f.contentWindow.postMessage({ __paperflux: "send", text: msg }, "*");
            dispatched++;
          } catch (e) {}
        }
        return dispatched > 0 ? "sent-iframe" : "no-socket";
      },
      ping() {
        if (state.socket && state.socket.readyState === 1) return "ok";
        return "waiting";
      },
      _debug() {
        return "top adopted=" + state.adopted +
          " current=" + (state.socket ? state.socket.readyState : "none") +
          " iframes=" + document.querySelectorAll("iframe").length;
      },
    };
    window.__paperflux.ready = true;
    return "ready";
  }

  // ---- child frame: adopt socket, forward to parent ----
  if (window.__paperfluxFrameReady) return "ready";
  window.__paperfluxFrameReady = true;

  const ChildWS = window.WebSocket;
  const cstate = { socket: null, adopted: 0 };

  function isCursorFrame(text) {
    if (typeof text !== "string") return false;
    return text.indexOf('"type":"cursor"') !== -1 && text.indexOf('"cursor"') !== -1;
  }

  function forward(text) {
    if (!isCursorFrame(text)) return;
    try {
      window.parent.postMessage({ __paperflux: "cursor", text: text }, "*");
    } catch (e) {}
  }

  function PaperFluxWS(url, protocols) {
    const ws = protocols === undefined ? new ChildWS(url) : new ChildWS(url, protocols);
    cstate.socket = ws;
    cstate.adopted++;
    ws.addEventListener("message", (ev) => forward(ev.data));
    return ws;
  }
  PaperFluxWS.prototype = ChildWS.prototype;
  PaperFluxWS.CONNECTING = ChildWS.CONNECTING;
  PaperFluxWS.OPEN = ChildWS.OPEN;
  PaperFluxWS.CLOSING = ChildWS.CLOSING;
  PaperFluxWS.CLOSED = ChildWS.CLOSED;
  window.WebSocket = PaperFluxWS;

  // The top frame asks us to send a cursor line.
  window.addEventListener("message", (ev) => {
    if (!ev || !ev.data || typeof ev.data !== "object") return;
    if (ev.data.__paperflux === "send" && typeof ev.data.text === "string") {
      if (cstate.socket && cstate.socket.readyState === 1) {
        try { cstate.socket.send(ev.data.text); } catch (e) {}
      }
    }
  });

  return "ready";
})()
`

// wrapCursorMessage builds the exact JSON the raw transports emit:
//
//	42["message",{"type":"cursor","cursor":"<len>;<base64>"}]
func wrapCursorMessage(decodedLen int, b64 string) string {
	return fmt.Sprintf(`42["message",{"type":"cursor","cursor":"%d;%s"}]`, decodedLen, b64)
}

// evaluateSendScript builds the JS call that forwards one base64 cursor
// frame through the tab's editor socket.
func evaluateSendScript(cursorField string) string {
	return fmt.Sprintf(`window.__paperflux && window.__paperflux.ready
		? window.__paperflux.sendCursor(%q)
		: "not-ready"`, cursorField)
}

// evaluatePingScript reports whether the tab already has an editor socket.
func evaluatePingScript() string {
	return `window.__paperflux && window.__paperflux.ready
		? window.__paperflux.ping() + " " + window.__paperflux._debug()
		: "not-installed"`
}
