package transport

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

type cursorLimitedWire struct{ fakeTransport }

func (*cursorLimitedWire) MaxSendSize() int { return 3000 }

func TestEncryptedBatchFitsCursorCarrierWithoutLosingPackets(t *testing.T) {
	raw := &cursorLimitedWire{}
	enc, err := NewEncryptedTransport(raw, "bounded-test-secret-32-characters", "carrier-size-test", false)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewEncryptedTransport(&fakeTransport{}, "bounded-test-secret-32-characters", "carrier-size-test", true)
	if err != nil {
		t.Fatal(err)
	}
	batch := NewBatchedTransport(enc)
	batch.lingerMs = 20
	if err = batch.Start(); err != nil {
		t.Fatal(err)
	}
	defer batch.Stop()
	var expected [][]byte
	for i := 0; i < 24; i++ {
		p := make([]byte, 1500)
		_, _ = rand.Read(p)
		expected = append(expected, p)
		if err = batch.Send(p); err != nil {
			t.Fatal(err)
		}
	}
	var got [][]byte
	receiver.Receive(func(frame []byte) {
		pkts, e := decodeBatch(frame)
		if e != nil {
			t.Fatal(e)
		}
		got = append(got, pkts...)
	})
	deadline := time.Now().Add(2 * time.Second)
	consumed := 0
	for len(got) < len(expected) && time.Now().Before(deadline) {
		raw.mu.Lock()
		frames := append([][]byte(nil), raw.sent[consumed:]...)
		consumed = len(raw.sent)
		raw.mu.Unlock()
		for _, frame := range frames {
			if len(frame) > 3000 {
				t.Fatalf("carrier limit exceeded: encrypted batch=%d >3000", len(frame))
			}
			receiver.Transport.(*fakeTransport).cb(frame)
		}
		time.Sleep(time.Millisecond)
	}
	if len(got) != len(expected) {
		t.Fatalf("delivered %d of %d", len(got), len(expected))
	}
	for i := range expected {
		if !bytes.Equal(got[i], expected[i]) {
			t.Fatal("packet order/integrity changed")
		}
	}
}

func TestCarrierBatchBudgetUsesEncodedSizeAndRejectsOversizedRecord(t *testing.T) {
	raw := &cursorLimitedWire{}
	b := NewBatchedTransport(raw)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	if err := b.Send(bytes.Repeat([]byte("a"), 65000)); err != nil {
		t.Fatal("compressible record should fit", err)
	}
	large := make([]byte, 5000)
	_, _ = rand.Read(large)
	if err := b.Send(large); err == nil {
		t.Fatal("oversized incompressible record admitted")
	}
	packets := [][]byte{bytes.Repeat([]byte("a"), 1500), bytes.Repeat([]byte("b"), 1500), bytes.Repeat([]byte("c"), 1500)}
	if chunks := encodeCarrierBatches(packets, 3000); len(chunks) != 1 {
		t.Fatal("compressed small batch unnecessarily split")
	}
	if unlimited := NewBatchedTransport(&fakeTransport{}); unlimited.maxWireBytes != 0 {
		t.Fatal("unlimited carrier changed")
	}
}
