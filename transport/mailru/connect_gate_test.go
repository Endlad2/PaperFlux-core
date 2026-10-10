package mailru

import (
	"context"
	"testing"
	"time"
)

func TestProviderGateOnlyPausesRepeatedProviderRejections(t *testing.T) {
	now := time.Now()
	g := mailruConnectGate{slots: make(chan struct{}, 1)}
	for _, status := range []int{0, 400, 401, 403, 404} {
		g.rejected(status, "", now)
	}
	if !g.retryAt.IsZero() {
		t.Fatal("individual document/network failures paused provider")
	}
	g.rejected(409, "", now)
	g.rejected(409, "", now)
	if !g.retryAt.IsZero() {
		t.Fatal("isolated conflict paused provider")
	}
	g.rejected(409, "", now)
	if !g.retryAt.Equal(now.Add(30 * time.Second)) {
		t.Fatal("repeated provider rejection not bounded")
	}
	g.rejected(429, "120", now)
	if !g.retryAt.Equal(now.Add(120 * time.Second)) {
		t.Fatal("Retry-After ignored")
	}
	g.rejected(429, "99999999", now)
	if !g.retryAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatal("unbounded provider pause")
	}
}

func TestProviderGateCancelledWaitDoesNotLeakSlots(t *testing.T) {
	g := mailruConnectGate{slots: make(chan struct{}, 1)}
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = g.acquire(ctx); err == nil {
		t.Fatal("cancelled blocked connector acquired slot")
	}
	release()
	g.retryAt = time.Now().Add(time.Minute)
	if _, err = g.acquire(ctx); err == nil || len(g.slots) != 0 {
		t.Fatal("cancelled provider wait leaked slot")
	}
	g.retryAt = time.Time{}
	release, err = g.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
