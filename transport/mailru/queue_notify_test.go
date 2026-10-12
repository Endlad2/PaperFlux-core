package mailru

import (
	"errors"
	"testing"
	"time"

	"universal-bypass-tool/transport"
)

func TestMailruWriterNotifiesWhenBoundedQueueDrains(t *testing.T) {
	t.Setenv("PAPERFLUX_MAILRU_QUEUE_NOTIFY", "")
	tr := NewMailruDocsTransport("owned-placeholder/not-used", transport.DefaultConfig())
	if err := tr.BaseTransport.Start(); err != nil {
		t.Fatal(err)
	}
	defer tr.Stop()
	tr.writeQueue = make(chan []byte, 1)
	tr.writeSpace = make(chan struct{}, 1)
	tr.session = &DocSession{WriteQueue: tr.writeQueue}
	tr.SetConnected(true)
	if err := tr.Send([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Send([]byte("next")); !errors.Is(err, transport.ErrSendQueueFull) {
		t.Fatal("queue-full did not reject without admission", err)
	}
	go tr.writerLoop()
	select {
	case <-tr.SendReady():
	case <-time.After(time.Second):
		t.Fatal("writer did not notify queue space")
	}
	if len(tr.writeQueue) != 0 {
		t.Fatal("queue grew instead of draining")
	}
	t.Setenv("PAPERFLUX_MAILRU_QUEUE_NOTIFY", "legacy")
	if tr.SendReady() != nil {
		t.Fatal("legacy comparison override ignored")
	}
}
