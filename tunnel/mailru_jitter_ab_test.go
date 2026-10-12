package tunnel

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

// Lossless ordered stand-in isolates spurious RTO from Internet/service load.
// One downlink pause starts at the first data packet, not at the handshake.
type jitterPolicyWire struct {
	recoveryWire
	mu          sync.Mutex
	cb          func([]byte)
	q           chan []byte
	ctx         context.Context
	peer        *jitterPolicyWire
	pause       bool
	dataStarted bool
}

func (*jitterPolicyWire) TCPTransportProfile() string { return "mailru" }
func (w *jitterPolicyWire) Receive(cb func([]byte))   { w.mu.Lock(); w.cb = cb; w.mu.Unlock() }
func (w *jitterPolicyWire) Send(data []byte) error {
	select {
	case w.q <- append([]byte(nil), data...):
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}
func (w *jitterPolicyWire) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case data := <-w.q:
			delay := 40 * time.Millisecond
			if w.pause && !w.dataStarted && len(data) > 40 {
				ihl := int(data[0]&15) * 4
				if ihl >= 20 && len(data) > ihl+20 && len(data) > ihl+int(data[ihl+12]>>4)*4 {
					w.dataStarted = true
					delay += 800 * time.Millisecond
				}
			}
			timer := time.NewTimer(delay)
			select {
			case <-w.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			w.peer.mu.Lock()
			cb := w.peer.cb
			w.peer.mu.Unlock()
			if cb != nil {
				cb(data)
			}
		}
	}
}
func TestMailruOrderedDelayPolicyAB(t *testing.T) {
	if os.Getenv("PAPERFLUX_TEST_MAILRU_JITTER") != "1" {
		t.Skip("explicit bounded timing experiment")
	}
	for _, mode := range []string{"legacy", "cubic"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("PAPERFLUX_MAILRU_TCP_PRESET", mode)
			t.Setenv("PAPERFLUX_TCP_RECOVERY", "classic")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &jitterPolicyWire{ctx: ctx, q: make(chan []byte, 4096)}
			b := &jitterPolicyWire{ctx: ctx, q: make(chan []byte, 4096), pause: true}
			a.peer = b
			b.peer = a
			go a.run()
			go b.run()
			client := NewTCPTunnel(a, false)
			defer client.Close()
			server := NewTCPTunnel(b, false)
			defer server.Close()
			ln, err := server.ListenTCP(9000)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			const size = 256 << 10
			written := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					written <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(25 * time.Second))
				_, err = conn.Write(make([]byte, size))
				if err == nil {
					err = conn.(*gonet.TCPConn).CloseWrite()
				}
				written <- err
			}()
			conn, err := client.DialTCP("10.10.10.2:9000")
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(25 * time.Second))
			start := time.Now()
			n, err := io.Copy(io.Discard, conn)
			elapsed := time.Since(start)
			if err != nil || n != size {
				t.Fatalf("download incomplete bytes=%d error=%v", n, err)
			}
			if err = <-written; err != nil {
				t.Fatal(err)
			}
			st := server.gvisorStack.Stats().TCP
			t.Logf("mode=%s bytes=%d elapsed_ms=%d timeouts=%d retransmits=%d", mode, n, elapsed.Milliseconds(), st.Timeouts.Value(), st.Retransmits.Value())
		})
	}
}
