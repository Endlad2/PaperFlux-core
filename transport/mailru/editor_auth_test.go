package mailru

import (
	"encoding/json"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestEditorReadyOnlyAfterSuccessfulCurrentAuth(t *testing.T) {
	s, _, _ := mailruTestSocket(t)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	for _, frame := range []string{`42["message",{"type":"auth"}]`, `42["other",{"type":"auth","result":1}]`, `42["message",{"type":"cursor","data":{"type":"auth","result":1}}]`} {
		tr.handleMessage(s, []byte(frame))
		if tr.IsConnected() {
			t.Fatal("unconfirmed editor marked ready")
		}
	}
	tr.handleMessage(&DocSession{}, []byte(`42["message",{"type":"auth","result":1}]`))
	if tr.IsConnected() {
		t.Fatal("stale auth marked ready")
	}
	tr.handleMessage(s, []byte(`42["message",{"type":"auth","result":1}]`))
	if !tr.IsConnected() || !s.authenticated.Load() {
		t.Fatal("current successful auth not ready")
	}
	tr.handleMessage(s, []byte(`42["message",{"type":"disconnectReason","code":4007}]`))
	if tr.IsConnected() {
		t.Fatal("rejected editor still ready")
	}
}

func TestCoauthoringUnlockOnlyOnCurrentWaitAuth(t *testing.T) {
	s, received, _ := mailruTestSocket(t)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	for _, frame := range []string{`42["message",{"type":"connectState"}]`, `42["message",{"type":"connectState","waitAuth":false}]`, `42["other",{"type":"connectState","waitAuth":true}]`, `42["message",{"type":"cursor","data":{"type":"connectState","waitAuth":true}}]`} {
		tr.handleMessage(s, []byte(frame))
	}
	tr.handleMessage(&DocSession{}, []byte(`42["message",{"type":"connectState","waitAuth":true}]`))
	select {
	case <-received:
		t.Fatal("unexpected unlock")
	default:
	}
	tr.handleMessage(s, []byte(`42["message",{"type":"connectState","waitAuth":true}]`))
	select {
	case frame := <-received:
		var event []json.RawMessage
		if json.Unmarshal([]byte(frame[2:]), &event) != nil {
			t.Fatal("invalid event")
		}
		var command map[string]interface{}
		_ = json.Unmarshal(event[1], &command)
		if len(command) != 4 || command["type"] != "unLockDocument" || command["unlock"] != true || command["isSave"] != false || command["releaseLocks"] != false {
			t.Fatal("unlock modifies document/locks")
		}
	case <-time.After(time.Second):
		t.Fatal("coauthoring auth remains locked")
	}
	if tr.IsConnected() {
		t.Fatal("unlock is not authentication confirmation")
	}
}

func TestHeartbeatDoesNotExtendEditorAuthDeadline(t *testing.T) {
	s, _, _ := mailruTestSocket(t)
	s.authDeadline = time.Now().Add(40 * time.Millisecond)
	// No server auth arrives; a negotiated 90s idle budget must not apply.
	started := time.Now()
	_, _, err := s.readMessage()
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("unbounded editor authentication")
	}
}

func TestConfirmedAuthDropsInitialDeadline(t *testing.T) {
	s, _, _ := mailruTestSocket(t)
	s.authDeadline = time.Now().Add(-time.Second)
	s.authenticated.Store(true)
	s.readTimeout = 30 * time.Millisecond
	started := time.Now()
	_, _, err := s.readMessage()
	if err == nil || time.Since(started) < 20*time.Millisecond || time.Since(started) > time.Second {
		t.Fatal("confirmed editor used initial auth deadline instead of idle budget")
	}
}
