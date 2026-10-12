package mailru

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestMailruCursorDeclaresActualBinaryLength(t *testing.T) {
	for _, size := range []int{0, 1, 8, 14, 18, 64, 1500, 65535} {
		data := bytes.Repeat([]byte{0xa5}, size)
		frame := mailruCursorMessage(data)
		var envelope []json.RawMessage
		if json.Unmarshal(frame[2:], &envelope) != nil || len(envelope) != 2 {
			t.Fatal("invalid envelope")
		}
		var event struct {
			Type   string `json:"type"`
			Cursor string `json:"cursor"`
		}
		if json.Unmarshal(envelope[1], &event) != nil || event.Type != "cursor" {
			t.Fatal("invalid cursor event")
		}
		length, payload, ok := strings.Cut(event.Cursor, ";")
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if !ok || length != strconv.Itoa(size) || err != nil || !bytes.Equal(decoded, data) {
			t.Fatalf("invalid binary cursor size=%d", size)
		}
	}
}

func TestMailruNewHeartbeatDoesNotBecomeTunnelData(t *testing.T) {
	data := mailruCursorMessage([]byte("---KA---"))
	if !strings.Contains(string(data), `"cursor":"8;`+mailruEncodedKeepAlive+`"`) {
		t.Fatal("heartbeat is not a valid length-prefixed Base64 string")
	}
	if got := CursorPayloads(string(data)); len(got) != 0 {
		t.Fatal("heartbeat delivered as VPN packet")
	}
	// Continue receiving published clients' legacy packets during upgrades.
	got := CursorPayloads(`42["message",{"type":"cursor","messages":[{"cursor":"18;---KA---"},{"cursor":"8;LS0tS0EtLS0="},{"cursor":"3;YWJj"},{"cursor":"18;ZGVm"}]}]`)
	if len(got) != 2 || got[0] != "YWJj" || got[1] != "ZGVm" {
		t.Fatal("heartbeat erased adjacent data or legacy support")
	}
}
