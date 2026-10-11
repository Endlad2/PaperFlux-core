package mtslink

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestBaseInfoParser(t *testing.T) {
	body := []byte(`before const baseInfo = {boardUID: "board", clientUID: "client", wsDomain: "wss://wsboard.mts-link.ru", signature: "x, brokenKey: }\\\"", temporary: true};after`)
	i, e := extractBaseInfo(body)
	if e != nil || i.BoardUID != "board" || !i.Temporary {
		t.Fatalf("parse failed %v", e)
	}
	for _, body := range []string{`nothing`, `const baseInfo = {a: 1`, `const baseInfo = {boardUID:"b",clientUID:"c",wsDomain:"wss://evil.invalid"}`} {
		if _, e := extractBaseInfo([]byte(body)); e == nil {
			t.Fatal("invalid accepted")
		}
	}
}
func TestWireAndEcho(t *testing.T) {
	p := []byte{0, 3, 255}
	raw, _ := json.Marshal(fastView("peer", "sess", p))
	if !bytes.Equal(packetPayload(raw, "mine"), p) || packetPayload(raw, "peer") != nil {
		t.Fatal("cursor roundtrip/echo")
	}
	init := initMessage(baseInfo{BoardUID: "b", ClientUID: "c"})
	if init["board_uid"] != "b" || init["token"] != "c" || init["boardUID"] != nil {
		t.Fatal("init wire casing")
	}
	if packetPayload([]byte(`{"type":"fast","subtype":"view","data":{"cursorPosition":{"x":2}}}`), "mine") != nil {
		t.Fatal("human numeric cursor accepted")
	}
}
func TestURLBoundaries(t *testing.T) {
	if !ValidURL("https://boards.mts-link.ru/board/fixture") {
		t.Fatal("valid rejected")
	}
	for _, u := range []string{"https://boards.mts-link.ru.evil/board/x", "http://boards.mts-link.ru/board/x", "https://u@boards.mts-link.ru/board/x", "https://boards.mts-link.ru:443/board/x"} {
		if ValidURL(u) {
			t.Fatal("unsafe accepted")
		}
	}
}
func TestLocalHandshakeDataAndStop(t *testing.T) {
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		var m map[string]any
		if c.ReadJSON(&m) != nil || m["type"] != "init" || m["board_uid"] != "b" {
			return
		}
		_ = c.WriteJSON(map[string]any{"type": "initResponse", "sessionUID": "s"})
		for {
			var data map[string]any
			if c.ReadJSON(&data) != nil {
				return
			}
			if data["type"] == "fast" {
				data["data"].(map[string]any)["token"] = "other"
				_ = c.WriteJSON(data)
			}
		}
	}))
	defer server.Close()
	tpt := New("https://boards.mts-link.ru/board/test", transport.DefaultConfig())
	tpt.fetch = func(context.Context) (baseInfo, error) { return baseInfo{BoardUID: "b", ClientUID: "c"}, nil }
	tpt.dial = func(ctx context.Context, _ baseInfo) (*websocket.Conn, error) {
		c, _, e := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		return c, e
	}
	got := make(chan []byte, 1)
	tpt.Receive(func(p []byte) { got <- p })
	if e := tpt.Start(); e != nil {
		t.Fatal(e)
	}
	defer tpt.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for !tpt.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatal("no initResponse")
		}
		time.Sleep(time.Millisecond)
	}
	p := bytes.Repeat([]byte{0, 2, 4}, 500)
	if e := tpt.Send(p); e != nil {
		t.Fatal(e)
	}
	select {
	case reply := <-got:
		if !bytes.Equal(reply, p) {
			t.Fatal("payload mismatch")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	if e := tpt.Stop(); e != nil {
		t.Fatal(e)
	}
	if tpt.IsConnected() || tpt.Send(p) == nil {
		t.Fatal("stop did not close")
	}
}
