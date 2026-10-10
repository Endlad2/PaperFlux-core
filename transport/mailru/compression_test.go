package mailru

import (
	"bytes"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestGatewayCompressionNegotiationAndLargeCompressedHistory(t *testing.T) {
	frame := []byte(`42["message",{"type":"authChanges","changes":"` + strings.Repeat("history", 500000) + `"}]`)
	ack := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate") {
			w.WriteHeader(http.StatusConflict)
			return
		}
		c, err := (&websocket.Upgrader{EnableCompression: true}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		_ = c.WriteMessage(websocket.TextMessage, frame)
		_, p, err := c.ReadMessage()
		if err == nil {
			ack <- string(p)
		}
	}))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	old := newMailruWebSocketDialer()
	old.EnableCompression = false
	c, response, err := old.Dial(endpoint, nil)
	if c != nil {
		c.Close()
	}
	if err == nil || response == nil || response.StatusCode != 409 {
		t.Fatal("fixture did not reproduce missing-extension rejection")
	}
	response.Body.Close()
	fixed := newMailruWebSocketDialer()
	c, response, err = fixed.Dial(endpoint, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatal("production dialer did not negotiate required extension")
	}
	defer c.Close()
	c.EnableWriteCompression(false)
	c.SetReadLimit(maxMailruEditorFrameBytes)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, got, err := readMailruFrame(c, maxMailruEditorFrameBytes)
	if err != nil || !bytes.Equal(got, frame) {
		t.Fatal("compressed history corrupted or rejected")
	}
	s := &DocSession{Conn: c}
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	tr.handleMessage(s, got)
	select {
	case got := <-ack:
		if got != `42["message",{"type":"authChangesAck"}]` {
			t.Fatal("compressed history not acknowledged")
		}
	case <-time.After(time.Second):
		t.Fatal("ack missing")
	}
}

func TestDecompressedFrameLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{EnableCompression: true}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), 8192))
		_, _, _ = c.ReadMessage()
	}))
	defer server.Close()
	d := newMailruWebSocketDialer()
	c, _, err := d.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadLimit(4096) // Compressed wire is small; decompressed frame is not.
	_, data, err := readMailruFrame(c, 4096)
	if err == nil || data != nil || !strings.Contains(err.Error(), "decompressed limit") {
		t.Fatal("compression bypassed memory bound")
	}
}
