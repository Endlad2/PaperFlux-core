package mailru

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

// Bounded, no edits: isolate commands on an explicitly supplied owner document.
// Never print URLs, JWTs, cookies, user IDs, document contents or raw errors.
func TestOwnedMailruRejectionStages(t *testing.T) {
	raw := os.Getenv("PAPERFLUX_OWNED_MAILRU_TEST_URL")
	if raw == "" {
		t.Skip("owner-only live test")
	}
	tr := NewMailruDocsTransport(raw, transport.DefaultConfig())
	info, err := tr.fetchDocInfo(tr.weblink)
	if err != nil {
		reason, status := mailruFailureDetails(err)
		t.Fatalf("metadata reason=%s http=%d", reason, status)
	}
	t.Logf("metadata_edit=%t", canEdit(info.Permissions))
	headers := http.Header{}
	setMailruBrowserHeaders(headers)
	headers.Set("Origin", "https://docs.datacloudmail.ru")
	dialer := newMailruWebSocketDialer()
	conn, resp, err := dialer.DialContext(context.Background(), info.WsURL, headers)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		t.Fatalf("upgrade http=%d", status)
	}
	defer conn.Close()
	conn.EnableWriteCompression(false)
	conn.SetReadLimit(maxMailruEditorFrameBytes)
	s := &DocSession{Info: info, Conn: conn, authDeadline: time.Now().Add(45 * time.Second)}
	if err := waitMailruSocketIO(s, info.Token); err != nil {
		t.Fatal("socket handshake failed")
	}
	auth := map[string]interface{}{
		"type": "auth", "docid": info.DocKey, "documentCallbackUrl": info.CallbackURL, "token": "fghhfgsjdgfjs",
		"user":       map[string]interface{}{"id": info.EditorUserID, "username": randUserID(), "indexUser": -1},
		"editorType": 0, "lastOtherSaveTime": -1, "block": []interface{}{}, "documentFormatSave": 65, "view": false, "isCloseCoAuthoring": false,
		"openCmd": map[string]interface{}{"c": "open", "id": info.DocKey, "userid": info.EditorUserID, "format": info.FileType, "url": info.DocURL, "title": info.DocTitle, "lcid": 25, "nobase64": true, "convertToOrigin": ".pdf.xps.oxps.djvu"},
		"lang":    "ru", "mode": "edit", "permissions": info.Permissions, "IsAnonymousUser": false, "timezoneOffset": -180, "coEditingMode": "fast", "jwtOpen": info.Token, "time": 1000, "supportAuthChangesAck": true,
	}
	encoded, _ := json.Marshal([]interface{}{"message", auth})
	if s.safeWrite(websocket.TextMessage, append([]byte("42"), encoded...)) != nil {
		t.Fatal("auth write failed")
	}
	stage := "auth"
	authAt := time.Time{}
	started := time.Now()
	// Reader owns all deadlines; timers only perform writes.
	incoming := make(chan []byte, 4)
	failed := make(chan struct{}, 1)
	go func() {
		for {
			_, frame, err := s.readMessage()
			if err != nil {
				failed <- struct{}{}
				return
			}
			select {
			case incoming <- frame:
			case <-time.After(time.Second):
				return
			}
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for time.Since(started) < 45*time.Second {
		select {
		case frame := <-incoming:
			if string(frame) == "2" {
				s.safeWrite(websocket.TextMessage, []byte("3"))
				continue
			}
			e := parseMailruEditorEvent(frame)
			switch e.Type {
			case "authChanges":
				s.safeWrite(websocket.TextMessage, []byte(`42["message",{"type":"authChangesAck"}]`))
			case "connectState":
				if e.WaitAuth {
					s.safeWrite(websocket.TextMessage, []byte(`42["message",{"type":"unLockDocument","unlock":true,"isSave":false,"releaseLocks":false}]`))
				}
			case "auth":
				if e.Result != nil && *e.Result == 1 {
					authAt = time.Now()
					stage = "quiet"
					s.authenticated.Store(true)
					t.Log("stage=quiet auth_ok=true")
				}
			case "disconnectReason", "error", "drop":
				t.Fatalf("rejected stage=%s code=%d auth_age_ms=%d", stage, e.Code, time.Since(authAt).Milliseconds())
			}
		case <-failed:
			t.Fatalf("socket_closed stage=%s", stage)
		case <-tick.C:
			if authAt.IsZero() {
				continue
			}
			elapsed := time.Since(authAt)
			if stage == "quiet" && elapsed >= 5*time.Second {
				stage = "activity"
				s.safeWrite(websocket.TextMessage, []byte(`42["message",{"type":"extendSession","idletime":0}]`))
				t.Log("stage=activity")
			}
			if stage == "activity" && elapsed >= 10*time.Second {
				variant := os.Getenv("PAPERFLUX_CURSOR_PROBE")
				stage = "cursor"
				cursor := "18;---KA---"
				switch variant {
				case "quiet":
					stage = "quietcontrol"
				case "packet":
					cursor = "18;" + base64.StdEncoding.EncodeToString(make([]byte, 1500))
				case "valid":
					cursor = "14;BgAAADYAMgA3AAMAAAA="
				case "packet14":
					cursor = "14;" + base64.StdEncoding.EncodeToString(make([]byte, 1500))
				case "keepalive14":
					cursor = "14;---KA---"
				case "valid18":
					cursor = "18;BgAAADYAMgA3AAMAAAA="
				case "length1500":
					cursor = "1500;" + base64.StdEncoding.EncodeToString(make([]byte, 1500))
				case "length18":
					cursor = "18;" + base64.StdEncoding.EncodeToString(make([]byte, 18))
				}
				if variant != "quiet" {
					message, _ := json.Marshal([]interface{}{"message", map[string]string{"type": "cursor", "cursor": cursor}})
					s.safeWrite(websocket.TextMessage, append([]byte("42"), message...))
				}
				t.Logf("stage=%s variant=%s", stage, variant)
			}
			if elapsed >= 20*time.Second {
				t.Log("all_stages_survived=true")
				return
			}
		}
	}
	t.Fatalf("timeout stage=%s", stage)
}
