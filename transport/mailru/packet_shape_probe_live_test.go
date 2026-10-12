package mailru

import (
	"bytes"
	"crypto/rand"
	"os"
	"strconv"
	"testing"
	"time"
	"universal-bypass-tool/transport"
	"universal-bypass-tool/transport/profilesecure"
)

// Local diagnostic only: bounded traffic on the explicitly owned test document.
// No document edits, production peers, profile tokens, or payloads are logged.
func TestOwnedMailruPacketShapes(t *testing.T) {
	url := os.Getenv("PAPERFLUX_OWNED_MAILRU_TEST_URL")
	if url == "" {
		t.Skip("owned fixture required")
	}
	style := os.Getenv("PAPERFLUX_PACKET_PROBE_STYLE")
	if style != "random" && style != "pfs2" {
		t.Fatal("explicit probe style required")
	}
	var first transport.Transport = NewMailruDocsTransport(url, transport.DefaultConfig())
	var second transport.Transport = NewMailruDocsTransport(url, transport.DefaultConfig())
	if os.Getenv("PAPERFLUX_PACKET_PROBE_BATCHED") == "1" {
		encA, err := transport.NewEncryptedTransport(first, "bounded-diagnostic-token-32-characters", "owned-batching-probe", false)
		if err != nil {
			t.Fatal("encryption constructor failed")
		}
		encB, err := transport.NewEncryptedTransport(second, "bounded-diagnostic-token-32-characters", "owned-batching-probe", true)
		if err != nil {
			t.Fatal("encryption constructor failed")
		}
		first = transport.NewBatchedTransport(encA)
		second = transport.NewBatchedTransport(encB)
	}
	if os.Getenv("PAPERFLUX_PACKET_PROBE_SECURE") == "1" {
		var err error
		first, err = profilesecure.New(first, "bounded-diagnostic", "local-owned-fixture-only-token-32-characters", "mailru", false)
		if err != nil {
			t.Fatal("secure constructor failed")
		}
		second, err = profilesecure.New(second, "bounded-diagnostic", "local-owned-fixture-only-token-32-characters", "mailru", true)
		if err != nil {
			t.Fatal("secure constructor failed")
		}
	}
	got := make(chan []byte, 1024)
	second.Receive(func(data []byte) {
		select {
		case got <- append([]byte(nil), data...):
		default:
		}
	})
	for _, tr := range []transport.Transport{first, second} {
		if err := tr.Start(); err != nil {
			t.Fatal("test peer start failed")
		}
		t.Cleanup(func() { _ = tr.Stop() })
	}
	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) && !(first.IsConnected() && second.IsConnected()) {
		time.Sleep(100 * time.Millisecond)
	}
	if !first.IsConnected() || !second.IsConnected() {
		t.Fatal("test peer authentication timed out")
	}
	if os.Getenv("PAPERFLUX_PACKET_PROBE_BURST") == "1" {
		packets := make(map[string]bool)
		start := time.Now()
		for index := 0; index < 256; index++ {
			packet := make([]byte, 1595)
			_, _ = rand.Read(packet)
			packets[string(packet)] = true
			if err := first.Send(packet); err != nil {
				t.Fatal("burst enqueue failed")
			}
			time.Sleep(10 * time.Millisecond)
		}
		until := time.After(35 * time.Second)
		for len(packets) > 0 {
			select {
			case data := <-got:
				delete(packets, string(data))
			case <-until:
				t.Fatalf("burst remaining=%d reconnects=%d", len(packets), first.Stats().Reconnects+second.Stats().Reconnects)
			}
		}
		t.Logf("burst packets=256 elapsed_ms=%d reconnects=%d", time.Since(start).Milliseconds(), first.Stats().Reconnects+second.Stats().Reconnects)
		return
	}
	sizes := []int{110, 1595}
	count := 24
	if os.Getenv("PAPERFLUX_PACKET_PROBE_LARGE") == "1" {
		sizes = []int{4096, 8192, 16384, 32768, 65000}
		count = 2
	}
	if v, err := strconv.Atoi(os.Getenv("PAPERFLUX_PACKET_PROBE_SIZE")); err == nil && v > 0 && v <= 65000 {
		sizes = []int{v}
		count = 2
	}
	for _, size := range sizes {
		for index := 0; index < count; index++ {
			packet := make([]byte, size)
			_, _ = rand.Read(packet)
			if style == "pfs2" {
				copy(packet, "PFS2")
				packet[4] = 3
				packet[5] = 0
			}
			if err := first.Send(packet); err != nil {
				t.Fatalf("send_failed style=%s size=%d index=%d", style, size, index)
			}
			matched := false
			until := time.After(3 * time.Second)
			for !matched {
				select {
				case data := <-got:
					matched = bytes.Equal(data, packet)
				case <-until:
					t.Fatalf("delivery_failed style=%s size=%d index=%d connected_first=%t connected_second=%t", style, size, index, first.IsConnected(), second.IsConnected())
				}
			}
			time.Sleep(125 * time.Millisecond)
		}
		t.Logf("shape_verified style=%s size=%d packets=%d", style, size, count)
	}
	t.Logf("shape_complete style=%s reconnects_first=%d reconnects_second=%d", style, first.Stats().Reconnects, second.Stats().Reconnects)
}
