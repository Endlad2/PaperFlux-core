package browser

import (
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"universal-bypass-tool/transport"
)

// backendTransport is the transport.Transport implementation that every
// browser-backed lane uses.
type backendTransport struct {
	*transport.BaseTransport

	bridge   Bridge
	provider string
	urls     []string
	identity Identity

	tabs   []TabID
	closed atomic.Bool

	onDataMu sync.RWMutex
	onData   func([]byte)

	startTime time.Time

	// maxCursorBytes is the provider's per-message raw payload budget.
	maxCursorBytes int
}

func newBackend(bridge Bridge, cfg Config, provider string, urls []string, identity Identity) *backendTransport {
	maxCursor := 3000
	switch provider {
	case "yandex", "vyandex", "boards", "mtslink":
		maxCursor = 65535
	case "mailru":
		maxCursor = 3000
	default:
		maxCursor = 8192
	}
	return &backendTransport{
		BaseTransport:  transport.NewBaseTransport(transport.DefaultConfig()),
		bridge:         bridge,
		provider:       provider,
		urls:           urls,
		identity:       identity,
		maxCursorBytes: maxCursor,
		startTime:      time.Now(),
	}
}

func (t *backendTransport) Start() error {
	if err := t.BaseTransport.Start(); err != nil {
		return err
	}
	if err := t.bridge.Install(); err != nil {
		return fmt.Errorf("browser: install: %w", err)
	}
	if err := t.bridge.Launch(); err != nil {
		return fmt.Errorf("browser: launch: %w", err)
	}
	for _, url := range t.urls {
		id, err := t.bridge.OpenTab(url)
		if err != nil {
			for _, prev := range t.tabs {
				_ = t.bridge.CloseTab(prev)
			}
			t.tabs = nil
			return fmt.Errorf("browser: open %s: %w", url, err)
		}
		t.tabs = append(t.tabs, id)
		go t.forwardInbound(id)
	}
	log.Printf("[BROWSER] %s transport ready with %d document tab(s)", t.provider, len(t.tabs))
	t.SetConnected(true)
	return nil
}

func (t *backendTransport) Stop() error {
	if t.closed.Swap(true) {
		return nil
	}
	t.SetConnected(false)
	for _, id := range t.tabs {
		_ = t.bridge.CloseTab(id)
	}
	t.tabs = nil
	return t.BaseTransport.Stop()
}

// Send encodes one framed payload as a cursor line and writes it through the
// editor's own WebSocket.
func (t *backendTransport) Send(data []byte) error {
	if t.closed.Load() {
		return fmt.Errorf("browser: transport stopped")
	}
	if len(data) > t.maxCursorBytes {
		return fmt.Errorf("browser: frame %d exceeds %s cursor budget %d",
			len(data), t.provider, t.maxCursorBytes)
	}
	if len(t.tabs) == 0 {
		return fmt.Errorf("browser: no open tab")
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	line := fmt.Sprintf(`42["message",{"type":"cursor","cursor":"%d;%s"}]`, len(data), b64)
	if err := t.bridge.Send(t.tabs[0], line); err != nil {
		return fmt.Errorf("browser: send: %w", err)
	}
	t.RecordSend(len(data))
	return nil
}

func (t *backendTransport) Receive(cb func([]byte)) {
	t.onDataMu.Lock()
	t.onData = cb
	t.onDataMu.Unlock()
}

func (t *backendTransport) IsConnected() bool {
	return !t.closed.Load() && t.BaseTransport.IsConnected() && len(t.tabs) > 0
}

func (t *backendTransport) Stats() transport.TransportStats {
	s := t.BaseTransport.Stats()
	s.Connected = t.IsConnected()
	s.Uptime = time.Since(t.startTime)
	return s
}

func (t *backendTransport) forwardInbound(id TabID) {
	ch := t.bridge.Inbound(id)
	for text := range ch {
		payload, ok := extractCursorPayload(text)
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil || len(decoded) == 0 {
			continue
		}
		t.RecordReceive(len(decoded))
		t.onDataMu.RLock()
		cb := t.onData
		t.onDataMu.RUnlock()
		if cb != nil {
			cb(decoded)
		}
	}
}

// extractCursorPayload pulls the base64 field out of a cursor line.
func extractCursorPayload(text string) (string, bool) {
	const marker = `"cursor":"`
	i := indexOf(text, marker)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(marker):]
	semi := indexByte(rest, ';')
	if semi < 0 {
		return "", false
	}
	payload := rest[semi+1:]
	end := indexByte(payload, '"')
	if end < 0 {
		return "", false
	}
	payload = payload[:end]
	if payload == "" || payload == "---KA---" {
		return "", false
	}
	return payload, true
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

var _ transport.Transport = (*backendTransport)(nil)
