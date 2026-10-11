package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"universal-bypass-tool/transport"
	"universal-bypass-tool/transport/control"
	"universal-bypass-tool/transport/mailru"
	yandex "universal-bypass-tool/transport/yandexsession"
	"universal-bypass-tool/utils"
)

// Explicit owner-only fixture; exercise the real encrypted Session, including
// client replacement while the exit remains up. Never use production profiles.
func TestOwnedSessionLifecycle(t *testing.T) {
	provider := os.Getenv("PAPERFLUX_OWNED_PROVIDER")
	url := os.Getenv("PAPERFLUX_OWNED_URL")
	if url == "" {
		t.Skip("explicit owned fixture required")
	}
	if os.Getenv("PAPERFLUX_OWNED_DEBUG") == "1" {
		utils.EnableDebug()
	}
	var cookies map[string]string
	_ = json.Unmarshal([]byte(os.Getenv("PAPERFLUX_OWNED_COOKIES")), &cookies)
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	secret := hex.EncodeToString(nonce[:])
	makeSession := func(exit bool) (*transport.Session, transport.Transport) {
		s, err := transport.NewSession(transport.PeerParameters{Capabilities: control.CapabilityIPv4 | control.CapabilityTCP | control.CapabilityUDP, MaxPacketSize: 65000}, exit)
		if err != nil {
			t.Fatal(err)
		}
		var raw transport.Transport
		switch provider {
		case "mailru":
			raw = mailru.NewMailruDocsTransport(url, transport.DefaultConfig())
		case "yandex":
			tr := yandex.NewYandexDocsTransport(url, transport.DefaultConfig())
			if err := tr.ApplyCookies(cookies); err != nil {
				t.Fatal("fixture cookies rejected")
			}
			raw = tr
		default:
			t.Fatal("unsupported owned provider")
		}
		s.SetHandshakeTimeout(55 * time.Second)
		s.SetRecoveryTiming(3*time.Second, 30*time.Second)
		if err := s.AddTransport("document", raw, secret, "owned-reconnect-test", 100); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Stop() })
		return s, raw
	}
	exit, exitRaw := makeSession(true)
	diagnostics := func(raw transport.Transport) string {
		if d, ok := raw.(interface{ CarrierDiagnostics() string }); ok {
			return d.CarrierDiagnostics()
		}
		return "unavailable"
	}
	if err := exit.Start(); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 3; round++ {
		client, clientRaw := makeSession(false)
		started := time.Now()
		if err := client.Start(); err != nil {
			t.Logf("exit_diag=%s client_diag=%s", diagnostics(exitRaw), diagnostics(clientRaw))
			t.Fatalf("round=%d handshake_failed=%v exit_carrier=%t client_carrier=%t exit_raw=%+v client_raw=%+v", round, err, exitRaw.IsConnected(), clientRaw.IsConnected(), exitRaw.Stats(), clientRaw.Stats())
		}
		t.Logf("round=%d ready_ms=%d", round, time.Since(started).Milliseconds())
		const count = 64
		var mu sync.Mutex
		seen := make(map[uint32]bool)
		done := make(chan struct{}, 1)
		exit.Receive(func(p []byte) { _ = exit.Send(p) })
		client.Receive(func(p []byte) {
			if len(p) != 1500 || !bytes.Equal(p[24:56], nonce[:]) {
				return
			}
			n := binary.BigEndian.Uint32(p[20:24])
			if n >= count {
				return
			}
			mu.Lock()
			seen[n] = true
			if len(seen) == count {
				select {
				case done <- struct{}{}:
				default:
				}
			}
			mu.Unlock()
		})
		for n := 0; n < count; n++ {
			p := make([]byte, 1500)
			_, _ = rand.Read(p)
			p[0] = 0x45
			p[9] = 17
			binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
			binary.BigEndian.PutUint32(p[20:24], uint32(n))
			copy(p[24:56], nonce[:])
			if err := client.Send(p); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case <-done:
			t.Logf("round=%d bidirectional_packets=%d complete=true", round, count)
		case <-time.After(35 * time.Second):
			mu.Lock()
			n := len(seen)
			mu.Unlock()
			t.Fatalf("round=%d bidirectional_packets=%d complete=false exit_raw=%+v client_raw=%+v", round, n, exitRaw.Stats(), clientRaw.Stats())
		}
		if err := client.Stop(); err != nil {
			t.Fatal(err)
		}
		pause := 2 * time.Second
		if os.Getenv("PAPERFLUX_OWNED_LONG_RECONNECT") == "1" && round == 0 {
			pause = 65 * time.Second
		}
		t.Logf("round=%d exit_diag=%s client_diag=%s pause_seconds=%d", round, diagnostics(exitRaw), diagnostics(clientRaw), int(pause.Seconds()))
		time.Sleep(pause)
	}
}
