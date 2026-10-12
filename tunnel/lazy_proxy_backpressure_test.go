package tunnel

import (
	"encoding/binary"
	"testing"
	"time"
)

func fullMailruProxyForTest(provider string) (*LazyProxy, []byte) {
	packet := make([]byte, 40)
	packet[0] = 0x45
	packet[9] = 6
	packet[32] = 0x50
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], []byte{10, 10, 10, 2})
	p := &LazyProxy{upstream: policyTransport{provider: provider}, ip: [4]byte{10, 10, 10, 2}, queue: make(chan sessionPacket, 1), queueLimit: 256 << 10, queueSpace: make(chan struct{}), done: make(chan struct{}), admitTimeout: time.Second}
	p.queue <- sessionPacket{epoch: 1, data: packet}
	p.queuedBytes = len(packet)
	p.latestEpoch.Store(1)
	return p, packet
}
func waitProxyAdmission(t *testing.T, p *LazyProxy) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for p.queueWaits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if p.queueWaits.Load() == 0 {
		t.Fatal("reader did not wait for bounded queue")
	}
}
func TestMailruProxyBurstWaitsForQueueInsteadOfDroppingTCP(t *testing.T) {
	p, packet := fullMailruProxyForTest("mailru")
	done := make(chan struct{})
	go func() { p.enqueueSession(1, packet); close(done) }()
	waitProxyAdmission(t, p)
	p.gate.Lock()
	old := <-p.queue
	p.queuedBytes -= len(old.data)
	close(p.queueSpace)
	p.queueSpace = make(chan struct{})
	p.gate.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queue release did not wake reader")
	}
	if p.queueDrops.Load() != 0 || len(p.queue) != 1 || p.queuedBytes != len(packet) {
		t.Fatal("TCP burst was lost or budget expanded")
	}
}
func TestMailruProxyQueueWaitCancelsAndRejectsObsoleteEpoch(t *testing.T) {
	for _, stop := range []bool{false, true} {
		p, packet := fullMailruProxyForTest("mailru")
		done := make(chan struct{})
		go func() { p.enqueueSession(1, packet); close(done) }()
		waitProxyAdmission(t, p)
		p.gate.Lock()
		if stop {
			p.closed = true
			close(p.done)
		} else {
			p.latestEpoch.Store(2)
			close(p.queueSpace)
			p.queueSpace = make(chan struct{})
		}
		p.gate.Unlock()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stopped/obsolete reader kept waiting")
		}
		if p.queueDrops.Load() != 0 || len(p.queue) != 1 {
			t.Fatal("cancellation changed queue")
		}
		if !stop && p.staleDrops.Load() != 1 {
			t.Fatal("obsolete packet admitted")
		}
	}
}
func TestMailruProxyQueueWaitIsBoundedAndProviderLocal(t *testing.T) {
	p, packet := fullMailruProxyForTest("mailru")
	p.admitTimeout = time.Millisecond
	p.enqueueSession(1, packet)
	if p.queueDrops.Load() != 1 || p.queueWaits.Load() != 1 {
		t.Fatal("timeout not accounted")
	}
	p, packet = fullMailruProxyForTest("yandex")
	p.enqueueSession(1, packet)
	if p.queueWaits.Load() != 0 || p.queueDrops.Load() != 1 {
		t.Fatal("policy leaked to other provider")
	}
}
