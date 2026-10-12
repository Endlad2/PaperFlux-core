package transport

import (
	"crypto/rand"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type pacedQueueCarrier struct {
	fakeTransport
	queue chan []byte
	ready chan struct{}
	done  chan struct{}
	event bool
	bytes atomic.Int64
}

func (*pacedQueueCarrier) MaxSendSize() int { return 2967 }
func (w *pacedQueueCarrier) SendReady() <-chan struct{} {
	if w.event {
		return w.ready
	}
	return nil
}
func (w *pacedQueueCarrier) Send(p []byte) error {
	select {
	case w.queue <- append([]byte(nil), p...):
		return nil
	default:
		return ErrSendQueueFull
	}
}
func (w *pacedQueueCarrier) Stop() error { close(w.done); return nil }
func (w *pacedQueueCarrier) consume() {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
		}
		select {
		case p := <-w.queue:
			select {
			case w.ready <- struct{}{}:
			default:
			}
			parts, err := decodeBatch(p)
			if err != nil {
				return
			}
			for _, part := range parts {
				w.bytes.Add(int64(len(part)))
			}
		default:
		}
	}
}
func TestBoundedBatchQueueWakeupAB(t *testing.T) {
	if os.Getenv("PAPERFLUX_TEST_QUEUE_AB") != "1" {
		t.Skip("explicit bounded queue timing test")
	}
	for _, event := range []bool{false, true, false, true} {
		w := &pacedQueueCarrier{queue: make(chan []byte, 16), ready: make(chan struct{}, 1), done: make(chan struct{}), event: event}
		b := NewBatchedTransport(w)
		if err := b.Start(); err != nil {
			t.Fatal(err)
		}
		go w.consume()
		const count, size = 256, 1478
		start := time.Now()
		for i := 0; i < count; i++ {
			p := make([]byte, size)
			rand.Read(p)
			if err := b.Send(p); err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(3 * time.Second)
		for w.bytes.Load() < count*size && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		elapsed := time.Since(start)
		st := b.Stats()
		b.Stop()
		if w.bytes.Load() != count*size || st.ExpiredDrops != 0 {
			t.Fatal("bounded queue lost data")
		}
		t.Logf("queue_event=%t bytes=%d elapsed_ms=%d retries=%d", event, w.bytes.Load(), elapsed.Milliseconds(), st.RetryQueued)
	}
}
