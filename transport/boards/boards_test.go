package boards

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestValidURL(t *testing.T) {
	for _, s := range []string{"https://boards.yandex.ru/whiteboard/?hash=abc123", "https://boards.yandex.ru/guest/?hash=a-b_c"} {
		if !ValidURL(s) {
			t.Fatal("valid rejected")
		}
	}
	for _, s := range []string{"http://boards.yandex.ru/whiteboard/?hash=a", "https://boards.yandex.ru.evil/whiteboard/?hash=a", "https://u@boards.yandex.ru/whiteboard/?hash=a", "https://boards.yandex.ru:443/whiteboard/?hash=a", "https://boards.yandex.ru/whiteboard/?hash=a&hash=b", "https://boards.yandex.ru/whiteboard/?hash=../../x"} {
		if ValidURL(s) {
			t.Fatal("unsafe accepted")
		}
	}
}
func TestPayloadAndSafeCleanup(t *testing.T) {
	p := []byte{0, 1, 2, 255}
	wire := buildModifyObjects(base64.StdEncoding.EncodeToString(p), "obj", 1, 1, "other", "other")
	raw, _ := json.Marshal(wire["data"])
	got, drop := ModifyObjectsPayloads(raw, "mine", "mine", "myname")
	if len(got) != 1 || !bytes.Equal(got[0], p) || len(drop) != 1 {
		t.Fatal("packet roundtrip")
	}
	got, drop = ModifyObjectsPayloads(raw, "other", "other", "")
	if len(got) != 0 || len(drop) != 0 {
		t.Fatal("own echo accepted")
	}
	data := wire["data"].(map[string]any)
	data["objects"].([]map[string]any)[0]["_attributes_"].(map[string]any)["style"] = "human text"
	raw, _ = json.Marshal(data)
	got, drop = ModifyObjectsPayloads(raw, "mine", "mine", "")
	if len(got) != 0 || len(drop) != 0 {
		t.Fatal("human object would be deleted")
	}
}
func TestAPIUsesJSON(t *testing.T) {
	r := apiRequest("fixture", "request-guest-token", map[string]string{"name": "guest"})
	if r.Header.Get("Content-Type") != "application/json" {
		t.Fatal("wrong API content type")
	}
}

// Explicit opt-in only. No owned board URLs or cookies are embedded in sources.
func TestOwnedBoardLive(t *testing.T) {
	raw := os.Getenv("PAPERFLUX_TEST_BOARD_URL")
	if raw == "" {
		t.Skip("owned board not provided")
	}
	a, b := NewBoardsTransport(raw, transport.DefaultConfig()), NewBoardsTransport(raw, transport.DefaultConfig())
	recv := make(chan []byte, 128)
	b.Receive(func(p []byte) { recv <- p })
	if e := a.Start(); e != nil {
		t.Fatal(e)
	}
	defer a.Stop()
	if e := b.Start(); e != nil {
		t.Fatal(e)
	}
	defer b.Stop()
	deadline := time.Now().Add(60 * time.Second)
	for !a.IsConnected() || !b.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatal("two board peers did not authorize")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, size := range []int{128, 1500, 8192} {
		p := bytes.Repeat([]byte("fixture"), size/7+1)[:size]
		if e := a.Send(p); e != nil {
			t.Fatal(e)
		}
		select {
		case got := <-recv:
			if !bytes.Equal(got, p) {
				t.Fatal("payload mismatch")
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("not delivered: %d bytes", size)
		}
		t.Log(fmt.Sprintf("delivered %d bytes", size))
	}
	// Force a real socket loss. Authorization must use a new guest session.
	old := a.session.Load()
	if old == nil {
		t.Fatal("missing connected session")
	}
	_ = old.Conn.Close()
	deadline = time.Now().Add(30 * time.Second)
	for a.session.Load() == old || a.session.Load() == nil || !a.IsConnected() {
		if time.Now().After(deadline) {
			t.Fatal("board did not reconnect")
		}
		time.Sleep(100 * time.Millisecond)
	}
	p := bytes.Repeat([]byte("reconnect"), 128)
	if e := a.Send(p); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-recv:
		if !bytes.Equal(got, p) {
			t.Fatal("reconnected payload mismatch")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reconnected payload not delivered")
	}
	t.Log("socket replacement authorized and delivered payload")
}
