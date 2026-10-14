// Package mobile implements the browser.Bridge over the existing IPC socket.
// Java (Android WebView) or Swift (WKWebView) owns the browser; Go only sends
// cursor lines and receives them back.
package mobile

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"universal-bypass-tool/browser"
	"universal-bypass-tool/transport/ipc"
)

// Bridge speaks to the host application's browser through the IPC client.
type Bridge struct {
	cfg    browser.Config
	client *ipc.Client

	mu          sync.Mutex
	tabs        map[browser.TabID]*tab
	next        int64
	ready       bool
	evalWaiters map[string]chan string

	corrSeq atomic.Int64
}

type tab struct {
	id      browser.TabID
	inbound chan string
	closed  chan struct{}
}

func New(cfg browser.Config, client *ipc.Client) *Bridge {
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 90 * time.Second
	}
	if cfg.InboundBuffer <= 0 {
		cfg.InboundBuffer = 4096
	}
	b := &Bridge{
		cfg:         cfg,
		client:      client,
		tabs:        make(map[browser.TabID]*tab),
		evalWaiters: make(map[string]chan string),
	}
	if client != nil {
		client.SetHandler(b.onMessage)
	}
	return b
}

func (b *Bridge) Install() error { return nil }

func (b *Bridge) Launch() error {
	if b.client == nil {
		return fmt.Errorf("mobile: no IPC client; host must call browser.New with one")
	}
	b.mu.Lock()
	b.ready = true
	b.mu.Unlock()
	return nil
}

func (b *Bridge) OpenTab(url string) (browser.TabID, error) {
	if b.client == nil {
		return "", browser.ErrUnsupported
	}
	b.mu.Lock()
	b.next++
	id := browser.TabID(fmt.Sprintf("mobile-%d", b.next))
	t := &tab{
		id:      id,
		inbound: make(chan string, b.cfg.InboundBuffer),
		closed:  make(chan struct{}),
	}
	b.tabs[id] = t
	b.mu.Unlock()

	req := &ipc.BrowserOpenTabPayload{TabID: string(id), URL: url}
	if err := b.client.Send(ipc.MsgBrowserOpenTab, req); err != nil {
		b.mu.Lock()
		delete(b.tabs, id)
		b.mu.Unlock()
		return "", fmt.Errorf("mobile: open tab: %w", err)
	}
	return id, nil
}

// Send forwards one cursor line to the host WebView.
func (b *Bridge) Send(id browser.TabID, line string) error {
	if b.client == nil {
		return browser.ErrUnsupported
	}
	if _, err := b.tab(id); err != nil {
		return err
	}
	return b.client.Send(ipc.MsgBrowserSend, &ipc.BrowserSendPayload{
		TabID: string(id),
		Line:  line,
	})
}

func (b *Bridge) Inbound(id browser.TabID) <-chan string {
	b.mu.Lock()
	t := b.tabs[id]
	b.mu.Unlock()
	if t == nil {
		closed := make(chan string)
		close(closed)
		return closed
	}
	return t.inbound
}

func (b *Bridge) CloseTab(id browser.TabID) error {
	b.mu.Lock()
	t := b.tabs[id]
	delete(b.tabs, id)
	b.mu.Unlock()
	if t == nil {
		return nil
	}
	close(t.closed)
	close(t.inbound)
	if b.client != nil {
		_ = b.client.Send(ipc.MsgBrowserCloseTab, &ipc.BrowserCloseTabPayload{TabID: string(id)})
	}
	return nil
}

func (b *Bridge) Close() error {
	b.mu.Lock()
	tabs := make([]*tab, 0, len(b.tabs))
	for _, t := range b.tabs {
		tabs = append(tabs, t)
	}
	b.tabs = make(map[browser.TabID]*tab)
	b.ready = false
	b.mu.Unlock()
	for _, t := range tabs {
		close(t.closed)
		close(t.inbound)
	}
	return nil
}

func (b *Bridge) onMessage(typ ipc.MsgType, payload []byte) {
	switch typ {
	case ipc.MsgBrowserInbound:
		var p ipc.BrowserInboundPayload
		if json.Unmarshal(payload, &p) != nil {
			return
		}
		b.mu.Lock()
		t := b.tabs[browser.TabID(p.TabID)]
		b.mu.Unlock()
		if t == nil {
			return
		}
		select {
		case t.inbound <- p.Frame:
		default:
		}
	case ipc.MsgBrowserEvalReply:
		var p ipc.BrowserEvalReplyPayload
		if json.Unmarshal(payload, &p) != nil {
			return
		}
		b.mu.Lock()
		reply := b.evalWaiters[p.CorrelationID]
		delete(b.evalWaiters, p.CorrelationID)
		b.mu.Unlock()
		if reply != nil {
			select {
			case reply <- p.Result:
			default:
			}
		}
	}
}

func (b *Bridge) tab(id browser.TabID) (*tab, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.tabs[id]
	if t == nil {
		return nil, browser.ErrTabClosed
	}
	select {
	case <-t.closed:
		return nil, browser.ErrTabClosed
	default:
	}
	return t, nil
}

var _ browser.Bridge = (*Bridge)(nil)
