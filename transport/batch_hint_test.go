package transport

import "testing"

type hintedBatch struct {
	Transport
	preferred int
}

func (h hintedBatch) PreferredBatchBytes() int { return h.preferred }

func TestCarrierBatchHintAndOverride(t *testing.T) {
	t.Setenv("OPENFLUX_BATCH_BYTES", "")
	plain := hintedBatch{preferred: 0}
	chat := hintedBatch{preferred: 500_000}
	if NewBatchedTransport(plain).maxBatchBytes != 8192 {
		t.Fatal("default changed")
	}
	if NewBatchedTransport(chat).maxBatchBytes != 500_000 {
		t.Fatal("hint ignored")
	}
	enc, err := NewEncryptedTransport(chat, "test-secret-32-characters-long", "hint", false)
	if err != nil {
		t.Fatal(err)
	}
	if NewBatchedTransport(enc).maxBatchBytes != 500_000 {
		t.Fatal("hint lost through encryption")
	}
	t.Setenv("OPENFLUX_BATCH_BYTES", "16384")
	if NewBatchedTransport(enc).maxBatchBytes != 16384 {
		t.Fatal("explicit override lost")
	}
}
