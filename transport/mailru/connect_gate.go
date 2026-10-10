package mailru

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Only new connections share this budget. Existing readers/writers and editor
// heartbeats never acquire it. A single bad document does not pause the host.
type mailruConnectGate struct {
	slots    chan struct{}
	mu       sync.Mutex
	failures int
	window   time.Time
	retryAt  time.Time
}

var mailruProviderGate = mailruConnectGate{slots: make(chan struct{}, 4)}

func (g *mailruConnectGate) acquire(ctx context.Context) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case g.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		g.mu.Lock()
		delay := time.Until(g.retryAt)
		g.mu.Unlock()
		if delay <= 0 {
			return func() { <-g.slots }, nil
		}
		<-g.slots
		timer := time.NewTimer(delay + time.Duration(rand.Int63n(int64(500*time.Millisecond))))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

func (g *mailruConnectGate) rejected(status int, retryAfter string, now time.Time) {
	if status != 409 && status != 429 && status < 500 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.window) > 30*time.Second {
		g.failures = 0
		g.window = now
	}
	g.failures++
	if status != 429 && g.failures < 3 {
		return
	}
	delay := 30 * time.Second
	if status == 429 {
		delay = time.Minute
	}
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		delay = max(delay, time.Duration(min(seconds, 900))*time.Second)
	} else if date, err := http.ParseTime(retryAfter); err == nil {
		delay = max(delay, min(date.Sub(now), 15*time.Minute))
	}
	deadline := now.Add(delay)
	if deadline.After(g.retryAt) {
		g.retryAt = deadline
	}
}
