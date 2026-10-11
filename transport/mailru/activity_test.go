package mailru

import (
	"encoding/json"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func assertActivityOnly(t *testing.T, frame string) {
	t.Helper()
	var envelope []json.RawMessage
	if len(frame) < 2 || frame[:2] != "42" || json.Unmarshal([]byte(frame[2:]), &envelope) != nil || len(envelope) != 2 {
		t.Fatal("invalid activity envelope")
	}
	var event string
	var body map[string]interface{}
	if json.Unmarshal(envelope[0], &event) != nil || event != "message" || json.Unmarshal(envelope[1], &body) != nil ||
		len(body) != 2 || body["type"] != "extendSession" || body["idletime"] != float64(0) {
		t.Fatal("activity edits document or uses wrong idle command")
	}
}

func TestMailruActivityOnlyForAuthenticatedCurrentSession(t *testing.T) {
	s, received, _ := mailruTestSocket(t)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	tr.SetConnected(true)
	if tr.extendEditorSession(s) {
		t.Fatal("unauthenticated activity sent")
	}
	s.authenticated.Store(true)
	if tr.extendEditorSession(&DocSession{}) {
		t.Fatal("stale activity sent")
	}
	s.Info.Permissions = map[string]interface{}{"edit": false}
	if !tr.extendEditorSession(s) {
		t.Fatal("read-only session cannot renew without edits")
	}
	select {
	case frame := <-received:
		assertActivityOnly(t, frame)
	case <-time.After(time.Second):
		t.Fatal("activity not sent")
	}
	tr.SetConnected(false)
	if tr.extendEditorSession(s) {
		t.Fatal("disconnected activity sent")
	}
}

func TestMailruActivityOnAuthAndIdleWarningOnly(t *testing.T) {
	s, received, _ := mailruTestSocket(t)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	tr.handleMessage(s, []byte(`42["message",{"type":"session","messages":{"code":4002,"reason":"idle session expires","interval":3600000}}]`))
	tr.handleMessage(s, []byte(`42["message",{"type":"auth","result":1}]`))
	select {
	case frame := <-received:
		assertActivityOnly(t, frame)
	case <-time.After(time.Second):
		t.Fatal("auth did not immediately renew activity")
	}
	for _, frame := range []string{
		`42["other",{"type":"session","code":4002}]`,
		`42["message",{"type":"cursor","nested":{"type":"session","code":4002}}]`,
		`42["message",{"type":"session","code":4003}]`,
		`42["message",{"type":"session","messages":{"code":4003}}]`,
	} {
		tr.handleMessage(s, []byte(frame))
	}
	tr.handleMessage(&DocSession{}, []byte(`42["message",{"type":"session","code":4002}]`))
	tr.handleMessage(s, []byte(`42["message",{"type":"session","messages":{"code":4002,"interval":3600000}}]`))
	select {
	case frame := <-received:
		assertActivityOnly(t, frame)
	case <-time.After(time.Second):
		t.Fatal("idle warning was ignored")
	}
	select {
	case <-received:
		t.Fatal("unrelated warning extended session")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMailruActivityLoopAcrossRotationAndStop(t *testing.T) {
	s, received, _ := mailruTestSocket(t)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	if err := tr.BaseTransport.Start(); err != nil {
		t.Fatal(err)
	}
	s.authenticated.Store(true)
	tr.session = s
	tr.SetConnected(true)
	finished := make(chan struct{})
	go func() { tr.editorActivityLoopEvery(5 * time.Millisecond); close(finished) }()
	t.Cleanup(func() { _ = tr.Stop() })
	// Accelerate the production activity loop, not a real-world endurance claim.
	for i := 0; i < 20; i++ {
		select {
		case frame := <-received:
			assertActivityOnly(t, frame)
		case <-time.After(time.Second):
			t.Fatal("periodic activity stopped")
		}
	}
	if !tr.dropSession(s) {
		t.Fatal("session did not rotate")
	}
	_ = s.Conn.Close()
	next, nextReceived, _ := mailruTestSocket(t)
	next.authenticated.Store(true)
	tr.Mu.Lock()
	tr.session = next
	tr.SetConnected(true)
	tr.Mu.Unlock()
	select {
	case frame := <-nextReceived:
		assertActivityOnly(t, frame)
	case <-time.After(time.Second):
		t.Fatal("replacement did not get activity")
	}
	_ = tr.Stop()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("activity loop survived Stop")
	}
}

func TestMailruActivityWriteFailureRetiresOnlyCurrentSession(t *testing.T) {
	s, _, _ := mailruTestSocket(t)
	s.authenticated.Store(true)
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.session = s
	tr.SetConnected(true)
	_ = s.Conn.Close()
	if tr.extendEditorSession(s) || tr.IsConnected() || tr.writeFailures.Load() != 1 {
		t.Fatal("failed activity did not retire session for its reader to reconnect")
	}
}
