package mailru

import (
	"bytes"
	"crypto/rand"
	"os"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

// Explicitly supplied owner test document only. No saveChanges loop is started:
// two real peers exercise auth/unlock/cursor packets without editing content.
func TestOwnedMailruGateway(t *testing.T) {
	raw := os.Getenv("PAPERFLUX_OWNED_MAILRU_TEST_URL")
	if raw == "" {
		t.Skip("owner-only live test")
	}
	first := NewMailruDocsTransport(raw, transport.DefaultConfig())
	second := NewMailruDocsTransport(raw, transport.DefaultConfig())
	received := make(chan []byte, 8)
	second.Receive(func(b []byte) {
		select {
		case received <- append([]byte(nil), b...):
		default:
		}
	})
	for _, tr := range []*MailruDocsTransport{first, second} {
		if err := tr.BaseTransport.Start(); err != nil {
			t.Fatal(err)
		}
		tr.writeQueue = make(chan []byte, 8)
		go tr.writerLoop()
		go tr.keepAliveLoop()
		tr.connectToDoc(0)
		t.Cleanup(func() { _ = tr.Stop() })
	}
	until := time.Now().Add(60 * time.Second)
	for !first.IsConnected() || !second.IsConnected() {
		if time.Now().After(until) {
			t.Fatal("editor authentication did not complete for both peers")
		}
		time.Sleep(100 * time.Millisecond)
	}
	packet := make([]byte, 1500)
	_, _ = rand.Read(packet)
	for n := 0; n < 12; n++ {
		if err := first.Send(packet); err != nil {
			t.Fatal("send failed")
		}
		deadline := time.NewTimer(3 * time.Second)
		matched := false
		for !matched {
			select {
			case got := <-received:
				matched = bytes.Equal(got, packet)
			case <-deadline.C:
				t.Fatal("packet lost/corrupted")
			}
		}
		deadline.Stop()
		time.Sleep(3 * time.Second)
		if !first.IsConnected() || !second.IsConnected() {
			t.Fatal("coauthoring carrier dropped before 36s")
		}
	}
	t.Log("two_peers_ready=true packets=12 lost=0 modified_document=false")
}
