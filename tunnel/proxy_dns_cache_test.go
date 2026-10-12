package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func proxyDNSReply(ttl uint32, release <-chan struct{}) (net.Conn, error) {
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		dc := &dns.Conn{Conn: server}
		q, err := dc.ReadMsg()
		if err != nil {
			return
		}
		if release != nil {
			<-release
		}
		answer := new(dns.Msg).SetReply(q)
		answer.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}, A: net.IPv4(192, 0, 2, 7)}}
		dc.WriteMsg(answer)
	}()
	return client, nil
}
func TestProxyDNSPositiveCacheExpiresAndIsBounded(t *testing.T) {
	now := time.Now()
	queries := 0
	p := &ProxyDialer{parent: context.Background(), now: func() time.Time { return now }, dial: func(string) (net.Conn, error) { return nil, nil }, probe: func(context.Context, string) (net.Conn, error) { queries++; return proxyDNSReply(120, nil) }}
	for _, host := range []string{"EXAMPLE.test:443", "example.test:80"} {
		if _, err := p.DialTCP(host); err != nil {
			t.Fatal(err)
		}
	}
	if queries != 1 {
		t.Fatal("repeat host queried DNS again")
	}
	now = now.Add(61 * time.Second)
	if _, err := p.DialTCP("example.test:443"); err != nil {
		t.Fatal(err)
	}
	if queries != 2 {
		t.Fatal("cached beyond TTL cap")
	}
	for i := 0; i < proxyDNSCacheLimit+10; i++ {
		if _, err := p.DialTCP(fmt.Sprintf("host%d.test:443", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.cache) > proxyDNSCacheLimit {
		t.Fatal("unbounded DNS memory")
	}
}
func TestProxyDNSZeroTTLAndFailedDialsAreNotKept(t *testing.T) {
	for _, ttl := range []uint32{0, 60} {
		queries := 0
		p := &ProxyDialer{parent: context.Background(), dial: func(string) (net.Conn, error) { return nil, fmt.Errorf("test dial unavailable") }, probe: func(context.Context, string) (net.Conn, error) { queries++; return proxyDNSReply(ttl, nil) }}
		for i := 0; i < 2; i++ {
			p.DialTCP("example.test:443")
		}
		if queries != 2 {
			t.Fatal("failed destination kept stale resolution")
		}
	}
	queries := 0
	p := &ProxyDialer{parent: context.Background(), dial: func(string) (net.Conn, error) { return nil, nil }, probe: func(context.Context, string) (net.Conn, error) { queries++; return proxyDNSReply(0, nil) }}
	for i := 0; i < 2; i++ {
		p.DialTCP("example.test:443")
	}
	if queries != 2 {
		t.Fatal("zero-TTL answer cached")
	}
}
func TestProxyDNSConcurrentSameHostHasOneTunneledQuery(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var queries atomic.Int32
	p := &ProxyDialer{parent: context.Background(), dial: func(string) (net.Conn, error) { return nil, nil }, probe: func(context.Context, string) (net.Conn, error) {
		if queries.Add(1) == 1 {
			close(entered)
		}
		return proxyDNSReply(60, release)
	}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.DialTCP("same.test:443"); err != nil {
				t.Error(err)
			}
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	if queries.Load() != 1 {
		t.Fatalf("duplicate DNS queries: %d", queries.Load())
	}
}
func TestProxyDNSBackupGetsFreshBudgetAndSelectedResolvers(t *testing.T) {
	t.Setenv("PAPERFLUX_DNS_PRIMARY", "192.0.2.1")
	t.Setenv("PAPERFLUX_DNS_SECONDARY", "192.0.2.2")
	calls := 0
	p := &ProxyDialer{parent: context.Background(), lookupTimeout: 100 * time.Millisecond, dial: func(string) (net.Conn, error) { return nil, nil }, probe: func(ctx context.Context, addr string) (net.Conn, error) {
		calls++
		if calls == 1 {
			if addr != "192.0.2.1:53" {
				t.Error(addr)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if addr != "192.0.2.2:53" || ctx.Err() != nil {
			t.Error("backup did not receive own live context", addr)
		}
		return proxyDNSReply(60, nil)
	}}
	if _, err := p.DialTCP("example.test:443"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("backup not attempted")
	}
}
func TestProxyDNSParentCancellationStopsBlockedQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	p := &ProxyDialer{parent: ctx, dial: func(string) (net.Conn, error) { t.Error("cancelled DNS opened target"); return nil, nil }, probe: func(context.Context, string) (net.Conn, error) {
		c, s := net.Pipe()
		go func() {
			defer s.Close()
			dc := &dns.Conn{Conn: s}
			if _, err := dc.ReadMsg(); err == nil {
				close(entered)
			}
			s.Read(make([]byte, 1))
		}()
		return c, nil
	}}
	go func() { _, err := p.DialTCP("example.test:443"); finished <- err }()
	<-entered
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancellation hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked DNS was not cancelled")
	}
}
