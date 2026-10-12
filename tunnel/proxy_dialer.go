package tunnel

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"universal-bypass-tool/utils"
)

// ProxyDialer resolves domain requests using DNS-over-TCP over the real data
// path. Physical-network DNS is used only for bootstrapping document providers.
type ProxyDialer struct {
	parent        context.Context
	dial          func(string) (net.Conn, error)
	probe         func(context.Context, string) (net.Conn, error)
	mu            sync.Mutex
	cache         map[string]proxyDNSCacheEntry
	flights       map[string]*proxyDNSFlight
	lookupTimeout time.Duration
	now           func() time.Time
}

type proxyDNSCacheEntry struct {
	ip      string
	expires time.Time
}
type proxyDNSFlight struct {
	done chan struct{}
	ip   string
	err  error
}

const proxyDNSCacheLimit = 256

func proxyDNSDiagnostic(result string) {
	if os.Getenv("PAPERFLUX_DIAGNOSTIC_PROXY_DNS") == "1" {
		utils.Infof("[PROXY_DNS] result=%s", result)
	}
}

func NewProxyDialer(t *TCPTunnel) *ProxyDialer {
	return &ProxyDialer{parent: t.lifecycleContext, dial: t.DialTCP, probe: t.DialProbe}
}

func (p *ProxyDialer) DialTCP(address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, fmt.Errorf("proxy supports IPv4 destinations only")
		}
		return p.dial(address)
	}
	key := strings.ToLower(dns.Fqdn(host))
	ip, err := p.resolve(key)
	if err != nil {
		return nil, err
	}
	conn, err := p.dial(net.JoinHostPort(ip, port))
	if err != nil {
		p.mu.Lock()
		delete(p.cache, key)
		p.mu.Unlock()
	}
	return conn, err
}

func (p *ProxyDialer) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *ProxyDialer) resolve(host string) (string, error) {
	if err := p.parent.Err(); err != nil {
		return "", err
	}
	p.mu.Lock()
	if entry, ok := p.cache[host]; ok && p.clock().Before(entry.expires) {
		p.mu.Unlock()
		proxyDNSDiagnostic("cache")
		return entry.ip, nil
	}
	if flight := p.flights[host]; flight != nil {
		p.mu.Unlock()
		proxyDNSDiagnostic("shared")
		select {
		case <-p.parent.Done():
			return "", p.parent.Err()
		case <-flight.done:
			return flight.ip, flight.err
		}
	}
	if len(p.flights) >= proxyDNSCacheLimit {
		// No new per-host bookkeeping under excessive parallel unique names;
		// retain the original bounded network lookup instead.
		p.mu.Unlock()
		ip, _, err := p.resolveUncached(host)
		return ip, err
	}
	if p.flights == nil {
		p.flights = make(map[string]*proxyDNSFlight)
	}
	flight := &proxyDNSFlight{done: make(chan struct{})}
	p.flights[host] = flight
	p.mu.Unlock()
	ip, ttl, err := p.resolveUncached(host)
	p.mu.Lock()
	if err == nil && ttl > 0 {
		if p.cache == nil {
			p.cache = make(map[string]proxyDNSCacheEntry)
		}
		if len(p.cache) >= proxyDNSCacheLimit {
			for key, entry := range p.cache {
				if !p.clock().Before(entry.expires) {
					delete(p.cache, key)
				}
			}
			if len(p.cache) >= proxyDNSCacheLimit {
				for key := range p.cache {
					delete(p.cache, key)
					break
				}
			}
		}
		p.cache[host] = proxyDNSCacheEntry{ip: ip, expires: p.clock().Add(ttl)}
	}
	flight.ip, flight.err = ip, err
	delete(p.flights, host)
	close(flight.done)
	p.mu.Unlock()
	return ip, err
}

func (p *ProxyDialer) resolveUncached(host string) (string, time.Duration, error) {
	proxyDNSDiagnostic("query")
	budget := p.lookupTimeout
	if budget <= 0 {
		budget = 4 * time.Second
	}
	var last error
	for _, server := range probeResolvers() {
		if err := p.parent.Err(); err != nil {
			return "", 0, err
		}
		// Fresh bounded budget for each resolver, never borrow the first's
		// exhausted deadline. All DNS still goes through the authenticated VPN.
		ctx, cancel := context.WithTimeout(p.parent, budget)
		conn, e := p.probe(ctx, server)
		if e != nil {
			last = e
			cancel()
			continue
		}
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		deadline, _ := ctx.Deadline()
		_ = conn.SetDeadline(deadline)
		dc := &dns.Conn{Conn: conn}
		q := new(dns.Msg).SetQuestion(dns.Fqdn(host), dns.TypeA)
		e = dc.WriteMsg(q)
		var answer *dns.Msg
		if e == nil {
			answer, e = dc.ReadMsg()
		}
		_ = conn.Close()
		stop()
		cancel()
		if e != nil {
			last = e
			continue
		}
		questionMatches := len(answer.Question) == 1 && answer.Question[0].Qtype == q.Question[0].Qtype && answer.Question[0].Qclass == q.Question[0].Qclass && strings.EqualFold(answer.Question[0].Name, q.Question[0].Name)
		if answer.Id != q.Id || !answer.Response || answer.Truncated || answer.Rcode != dns.RcodeSuccess || !questionMatches {
			last = fmt.Errorf("invalid proxy DNS response")
			continue
		}
		ttl := uint32(60)
		for _, record := range answer.Answer {
			if _, ok := record.(*dns.CNAME); ok && record.Header().Ttl < ttl {
				ttl = record.Header().Ttl
			}
		}
		for _, record := range answer.Answer {
			if a, ok := record.(*dns.A); ok {
				if a.A.To4() == nil {
					continue
				}
				if a.Hdr.Ttl < ttl {
					ttl = a.Hdr.Ttl
				}
				return a.A.String(), time.Duration(ttl) * time.Second, nil
			}
		}
		last = fmt.Errorf("proxy DNS returned no IPv4 address")
	}
	return "", 0, fmt.Errorf("tunneled DNS failed: %w", last)
}
