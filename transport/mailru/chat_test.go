package mailru

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"universal-bypass-tool/transport"
)

func TestChatWireRoundTripAndBounds(t *testing.T) {
	for _, size := range []int{1, 3000, 500_000, maxMailruChatPacketBytes} {
		data := bytes.Repeat([]byte{0x7f}, size)
		outgoing := mailruChatMessage(data)
		var envelope []json.RawMessage
		if json.Unmarshal(outgoing[2:], &envelope) != nil {
			t.Fatal("bad envelope")
		}
		var event struct {
			Type    string
			Message string
		}
		if json.Unmarshal(envelope[1], &event) != nil || event.Type != "message" {
			t.Fatal("not chat")
		}
		if event.Message != chatDataTag+base64.StdEncoding.EncodeToString(data) {
			t.Fatal("wire differs from upstream")
		}
		broadcast := []byte(fmt.Sprintf(`42["message",{"type":"message","messages":[{"message":%q,"useridoriginal":"peer"}]}]`, event.Message))
		got := DecodeChatData(broadcast, "self")
		if len(got) != 1 || !bytes.Equal(got[0], data) {
			t.Fatal("data mismatch")
		}
		if len(DecodeChatData(broadcast, "peer")) != 0 {
			t.Fatal("self echo delivered")
		}
	}
	for _, frame := range []string{
		`42["message",{"type":"auth","messages":[{"message":"OFX1YWJj"}]}]`,
		`42["other",{"type":"message","messages":[{"message":"OFX1YWJj"}]}]`,
		`42["message",{"type":"message","messages":[{"message":"hello"},{"message":"OFX1!"}]}]`,
		`42["message",{"type":"message","messages":[{"message":"OFX1` + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxMailruChatPacketBytes)+4) + `"}]}]`,
	} {
		if len(DecodeChatData([]byte(frame), "self")) != 0 {
			t.Fatal("invalid chat admitted")
		}
	}
	tr := NewMailruDocsTransport("unused/doc", transport.DefaultConfig())
	if err := tr.Send(make([]byte, maxMailruChatPacketBytes+1)); err == nil {
		t.Fatal("unbounded send")
	}
}

func TestChatReceiveOnlyCurrentSession(t *testing.T) {
	tr := NewMailruDocsTransport("unused/doc", transport.DefaultConfig())
	s := &DocSession{Info: MailruDocsInfo{EditorUserID: "self"}}
	tr.session = s
	count := 0
	tr.Receive(func(data []byte) {
		if string(data) != "abc" {
			t.Error("invalid packet")
		}
		count++
	})
	frame := []byte(`42["message",{"type":"message","messages":[{"message":"OFX1YWJj","useridoriginal":"peer"},{"message":"OFX1ZGVm","useridoriginal":"self"}]}]`)
	tr.handleMessage(&DocSession{}, frame)
	tr.handleMessage(s, frame)
	if count != 1 {
		t.Fatal("stale/self/peer dispatch wrong", count)
	}
}
