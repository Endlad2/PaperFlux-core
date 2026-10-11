package transport

import (
	"testing"
	"time"
)

func TestProfileBatchRetryIsBoundedAndLocal(t *testing.T) {
	a, b := &Session{}, &Session{}
	if err := a.SetBatchRetryInterval(10 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if a.newBatched(nil).retryInterval != 10*time.Millisecond || b.newBatched(nil).retryInterval != 0 {
		t.Fatal("retry policy leaked")
	}
	for _, invalid := range []time.Duration{0, -1, time.Second} {
		if err := b.SetBatchRetryInterval(invalid); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
	a.started = true
	if err := a.SetBatchRetryInterval(20 * time.Millisecond); err == nil {
		t.Fatal("changed active session")
	}
}
