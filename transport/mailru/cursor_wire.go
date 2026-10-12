package mailru

import (
	"encoding/base64"
	"strconv"
)

const mailruEncodedKeepAlive = "LS0tS0EtLS0="

// ONLYOFFICE's binary-string prefix is the decoded byte length, not a stream
// ID. A hard-coded 18 (and a non-Base64 heartbeat) is rejected by Mail.ru with
// 4004. The payload itself and the receiving protocol remain unchanged.
func mailruCursorMessage(data []byte) []byte {
	payload := base64.StdEncoding.EncodeToString(data)
	return []byte(`42["message",{"type":"cursor","cursor":"` + strconv.Itoa(len(data)) + `;` + payload + `"}]`)
}
