package mailru

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"universal-bypass-tool/transport"
)

// Explicit owned-document diagnostic: fixed budget, no document edits, no
// production profile negotiation. Measure the carrier, not the TCP stack.
func TestOwnedMailruCarrierCapacity(t *testing.T) {
	url := os.Getenv("PAPERFLUX_OWNED_MAILRU_TEST_URL")
	if url == "" {
		t.Skip("owned fixture required")
	}
	config := transport.DefaultConfig()
	if os.Getenv("PAPERFLUX_CAPACITY_GROUP_QUEUE") == "1" {
		config.MaxQueueSize = 16
	}
	var sender transport.Transport = NewMailruDocsTransport(url, config)
	var receiver transport.Transport = NewMailruDocsTransport(url, config)
	if os.Getenv("PAPERFLUX_CAPACITY_BATCHED") == "1" {
		a, err := transport.NewEncryptedTransport(sender, "bounded-test-secret-32-characters", "capacity-only-owned-fixture", false)
		if err != nil {
			t.Fatal("encrypt setup")
		}
		b, err := transport.NewEncryptedTransport(receiver, "bounded-test-secret-32-characters", "capacity-only-owned-fixture", true)
		if err != nil {
			t.Fatal("encrypt setup")
		}
		sender = transport.NewBatchedTransport(a)
		receiver = transport.NewBatchedTransport(b)
	}
	const count, size = 512, 1478
	var payloads [count][]byte
	for i := range payloads {
		payloads[i] = make([]byte, size)
		rand.Read(payloads[i])
		binary.BigEndian.PutUint32(payloads[i][:4], uint32(i))
	}
	got := make(chan []byte, count+128)
	receiver.Receive(func(p []byte) {
		select {
		case got <- p:
		default:
		}
	})
	for _, tr := range []transport.Transport{sender, receiver} {
		if err := tr.Start(); err != nil {
			t.Fatal("carrier start")
		}
		t.Cleanup(func() { tr.Stop() })
	}
	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) && !(sender.IsConnected() && receiver.IsConnected()) {
		time.Sleep(100 * time.Millisecond)
	}
	if !sender.IsConnected() || !receiver.IsConnected() {
		t.Fatal("carrier auth timeout")
	}
	start := time.Now()
	sent := make(chan bool, 1)
	go func() {
		for _, p := range payloads {
			if err := sender.Send(p); err != nil {
				sent <- false
				return
			}
		}
		sent <- true
	}()
	seen := make(map[uint32]bool)
	timeout := time.NewTimer(35 * time.Second)
	defer timeout.Stop()
	for len(seen) < count {
		select {
		case p := <-got:
			if len(p) != size {
				continue
			}
			id := binary.BigEndian.Uint32(p[:4])
			if id < count && bytes.Equal(p, payloads[id]) {
				seen[id] = true
			}
		case <-timeout.C:
			t.Fatalf("carrier delivered=%d/%d reconnects=%d", len(seen), count, sender.Stats().Reconnects+receiver.Stats().Reconnects)
		}
	}
	if !<-sent {
		t.Fatal("enqueue failed")
	}
	elapsed := time.Since(start)
	st := sender.Stats()
	t.Logf("capacity bytes=%d elapsed_ms=%d reconnects=%d write_failures=%d retries=%d queue_timeouts=%d", count*size, elapsed.Milliseconds(), st.Reconnects+receiver.Stats().Reconnects, st.WriteFailures, st.RetryQueued, st.QueueTimeouts)
}
