package transport

import (
	"bytes"
	"crypto/rand"
	"testing"

	"universal-bypass-tool/transport/control"
)

func TestCarrierPackingFillsConsecutiveFrames(t *testing.T) {
	// Two 1400-byte IPv4 packets plus their authenticated Session envelopes
	// fit in the 3000-byte cursor budget, including outer AES-GCM framing.
	var packets [][]byte
	for i := 0; i < 6; i++ {
		packet := make([]byte, 1400+control.EnvelopeSize)
		if _, err := rand.Read(packet); err != nil {
			t.Fatal(err)
		}
		packets = append(packets, packet)
	}
	frames := encodeCarrierBatches(packets, 3000-33)
	if len(frames) != 3 {
		t.Fatalf("underfilled frames: got %d, want 3", len(frames))
	}
	var decoded [][]byte
	for _, frame := range frames {
		if len(frame.data)+33 > 3000 {
			t.Fatal("encrypted carrier budget exceeded")
		}
		parts, err := decodeBatch(frame.data)
		if err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, parts...)
	}
	if len(decoded) != len(packets) {
		t.Fatal("records lost")
	}
	for i, packet := range packets {
		if !bytes.Equal(decoded[i], packet) {
			t.Fatal("packet order or data changed")
		}
	}
}
