package mailru

import (
	"bytes"
	"crypto/rand"
	"os"
	"strconv"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

// Opt-in owner fixture only. The production Start path now renews activity
// without saveChanges. No production server process/configuration is replaced.
func TestOwnedMailruEndurance(t *testing.T) {
	url := os.Getenv("PAPERFLUX_OWNED_MAILRU_TEST_URL")
	if url == "" {
		t.Skip("owned document required")
	}
	seconds, err := strconv.Atoi(os.Getenv("PAPERFLUX_MAILRU_ENDURANCE_SECONDS"))
	if err != nil || seconds < 60 || seconds > 10800 {
		t.Fatal("explicit bounded duration required")
	}
	first := NewMailruDocsTransport(url, transport.DefaultConfig())
	second := NewMailruDocsTransport(url, transport.DefaultConfig())
	received := make(chan []byte, 256)
	second.Receive(func(data []byte) {
		select {
		case received <- append([]byte(nil), data...):
		default:
		}
	})
	for _, tr := range []*MailruDocsTransport{first, second} {
		if err := tr.Start(); err != nil {
			t.Fatal("start failed")
		}
		t.Cleanup(func() { _ = tr.Stop() })
	}
	waitReady := func(budget time.Duration) bool {
		until := time.Now().Add(budget)
		for time.Now().Before(until) {
			if first.IsConnected() && second.IsConnected() {
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	if !waitReady(90 * time.Second) {
		t.Fatal("initial editor authentication failed")
	}
	t.Log("endurance_started=true peers=2 modified_document=false")
	start := time.Now()
	until := start.Add(time.Duration(seconds) * time.Second)
	nextProgress := start.Add(time.Minute)
	forced := false
	delivered := 0
	timeouts := 0
	for time.Now().Before(until) {
		if !first.IsConnected() || !second.IsConnected() {
			if !waitReady(90 * time.Second) {
				t.Fatal("automatic recovery failed")
			}
		}
		packet := make([]byte, 1500)
		_, _ = rand.Read(packet)
		if err := first.Send(packet); err != nil {
			t.Fatal("authenticated send failed")
		}
		deadline := time.NewTimer(10 * time.Second)
		matched := false
		for !matched {
			select {
			case got := <-received:
				matched = bytes.Equal(got, packet)
			case <-deadline.C:
				timeouts++
				matched = true
			}
		}
		deadline.Stop()
		if timeouts > 0 {
			t.Fatal("probe packet not delivered through document")
		}
		delivered++
		// Drop early, then keep the replacement alive for >1 hour in the 90m
		// run. A midpoint drop would only test two 45m sessions and miss the bug.
		if !forced && time.Since(start) > 30*time.Second {
			first.Mu.RLock()
			session := first.session
			first.Mu.RUnlock()
			if session == nil || session.Conn == nil {
				t.Fatal("no current session for forced drop")
			}
			before := first.Stats().Reconnects
			_ = session.Conn.Close()
			recoveryDeadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(recoveryDeadline) && !(first.IsConnected() && first.Stats().Reconnects > before) {
				time.Sleep(100 * time.Millisecond)
			}
			if !first.IsConnected() || first.Stats().Reconnects <= before {
				t.Fatal("forced drop did not reconnect")
			}
			forced = true
			t.Log("forced_reconnect=true")
		}
		if time.Now().After(nextProgress) {
			t.Logf("elapsed_seconds=%d packets=%d reconnects=%d", int(time.Since(start).Seconds()), delivered, first.Stats().Reconnects+second.Stats().Reconnects)
			nextProgress = nextProgress.Add(time.Minute)
		}
		time.Sleep(5 * time.Second)
	}
	if !forced {
		t.Fatal("forced reconnect not exercised")
	}
	t.Logf("endurance_complete=true duration_seconds=%d packets=%d lost=0 forced_reconnect=true", seconds, delivered)
}
