package yandex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestWriterDeclaresActualBinaryLength(t *testing.T) {
	got := make(chan []byte, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for i := 0; i < 3; i++ {
			_, p, e := c.ReadMessage()
			if e != nil {
				return
			}
			got <- p
		}
	}))
	defer server.Close()
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	tr := NewYandexDocsTransport("https://disk.yandex.ru/i/fixture", transport.DefaultConfig())
	_ = tr.BaseTransport.Start()
	defer tr.Stop()
	tr.session = &DocSession{Conn: c}
	tr.SetConnected(true)
	go tr.writerLoop()
	for _, n := range []int{18, 128, 1500} {
		p := bytes.Repeat([]byte{byte(n)}, n)
		tr.queue <- p
		select {
		case frame := <-got:
			var arr []json.RawMessage
			if e := json.Unmarshal(frame[2:], &arr); e != nil {
				t.Fatal(e)
			}
			var body struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(arr[1], &body)
			length, value, ok := strings.Cut(body.Cursor, ";")
			declared, _ := strconv.Atoi(length)
			actual, _ := base64.StdEncoding.DecodeString(value)
			if !ok || declared != n || !bytes.Equal(actual, p) {
				t.Fatal("declared packet length differs from payload")
			}
		case <-time.After(time.Second):
			t.Fatal("writer timed out")
		}
	}
}
