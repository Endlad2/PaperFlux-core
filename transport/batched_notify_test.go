package transport

import (
	"sync/atomic"
	"testing"
	"time"
)

type notifiedCarrier struct {
	fakeTransport
	ready  chan struct{}
	called chan struct{}
	writes atomic.Int32
}

func (w *notifiedCarrier) Send(p []byte) error {
	if w.writes.Add(1) == 1 {
		close(w.called)
		return ErrSendQueueFull
	}
	return w.fakeTransport.Send(p)
}
func (w *notifiedCarrier) SendReady() <-chan struct{} { return w.ready }

func TestBatchQueueNotificationAvoidsPollingDelay(t *testing.T) {
	raw := &notifiedCarrier{ready: make(chan struct{}, 1), called: make(chan struct{})}
	enc, err := NewEncryptedTransport(raw, "bounded-test-secret-32-characters", "notify-only-fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatchedTransport(enc)
	b.retryInterval = time.Hour
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	if err = b.Send([]byte("ordered-frame")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-raw.called:
	case <-time.After(time.Second):
		t.Fatal("send not attempted")
	}
	raw.ready <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for raw.sendCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if raw.sendCount() != 1 || b.Stats().RetryQueued != 1 {
		t.Fatal("queue notification did not bypass polling")
	}
}

func TestStoppingBatchCancelsQueueNotificationWait(t *testing.T) {
	raw := &notifiedCarrier{ready: make(chan struct{}), called: make(chan struct{})}
	b := NewBatchedTransport(raw)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	if err := b.Send([]byte("bounded")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-raw.called:
	case <-time.After(time.Second):
		t.Fatal("send not attempted")
	}
	done := make(chan struct{})
	go func() { b.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked on notification")
	}
}
