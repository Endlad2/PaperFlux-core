package mailru

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// OpenFlux v0.7.3 (f12e981): chat carries opaque encrypted batches instead
// of cursor/model updates. It persists in the document's chat history.
const chatDataTag = "OFX1"
const mailruPreferredBatchBytes = 500_000
const maxMailruChatPacketBytes = 768 << 10

func (*MailruDocsTransport) PreferredBatchBytes() int { return mailruPreferredBatchBytes }

func mailruChatMessage(data []byte) []byte {
	text := chatDataTag + base64.StdEncoding.EncodeToString(data)
	frame, _ := json.Marshal([]interface{}{"message", struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{"message", text}})
	return append([]byte("42"), frame...)
}

func decodeChatData(frame []byte, ownID string) [][]byte {
	if !bytes.HasPrefix(frame, []byte(`42["message",`)) || len(frame) > maxMailruEditorFrameBytes {
		return nil
	}
	var envelope []json.RawMessage
	if json.Unmarshal(frame[2:], &envelope) != nil || len(envelope) != 2 {
		return nil
	}
	var payload struct {
		Type     string `json:"type"`
		Messages []struct {
			Message        string `json:"message"`
			UserIDOriginal string `json:"useridoriginal"`
		} `json:"messages"`
	}
	if json.Unmarshal(envelope[1], &payload) != nil || payload.Type != "message" {
		return nil
	}
	var packets [][]byte
	for _, message := range payload.Messages {
		if ownID != "" && message.UserIDOriginal == ownID || !strings.HasPrefix(message.Message, chatDataTag) {
			continue
		}
		encoded := message.Message[len(chatDataTag):]
		if len(encoded) > base64.StdEncoding.EncodedLen(maxMailruChatPacketBytes) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && len(decoded) > 0 && len(decoded) <= maxMailruChatPacketBytes {
			packets = append(packets, decoded)
		}
	}
	return packets
}

func DecodeChatData(frame []byte, ownID string) [][]byte { return decodeChatData(frame, ownID) }
