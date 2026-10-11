// Package mtslink implements the wire protocol of trader52's official
// OpenFluxTransports mtslink v1.0.0: init, pingRequest, fast/view cursor packets.
// Protocol reference: p1neappleXpress/OpenFluxTransports at 5e24a882.
package mtslink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"universal-bypass-tool/netbind"
	"universal-bypass-tool/transport"
	"universal-bypass-tool/utils"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"
const maxFrame = 2 << 20

type baseInfo struct {
	BoardUID  string `json:"boardUID"`
	ClientUID string `json:"clientUID"`
	WSDomain  string `json:"wsDomain"`
	Prefix    string `json:"fePrefix"`
	Signature string `json:"signature"`
	Temporary bool   `json:"temporary"`
}

type session struct {
	conn    *websocket.Conn
	info    baseInfo
	uid     string
	done    chan struct{}
	writeMu sync.Mutex
}

func (s *session) write(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.conn.WriteJSON(v)
}

type Transport struct {
	*transport.BaseTransport
	url       string
	current   atomic.Pointer[session]
	lifecycle sync.Mutex
	cancel    context.CancelFunc
	stopped   chan struct{}
	// Overrides are private test seams, never profile-controlled endpoints.
	fetch func(context.Context) (baseInfo, error)
	dial  func(context.Context, baseInfo) (*websocket.Conn, error)
}

func New(raw string, cfg transport.TransportConfig) *Transport {
	t := &Transport{BaseTransport: transport.NewBaseTransport(cfg), url: raw}
	t.fetch = t.authorize
	t.dial = t.dialBoard
	return t
}

func ValidURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && len(raw) <= 512 && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" &&
		(u.Host == "boards.mts-link.ru" || u.Host == "my.mts-link.ru") && len(strings.Trim(u.Path, "/")) > 0
}
func validWSDomain(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "wss" && u.User == nil && u.Port() == "" && u.RawQuery == "" && u.Fragment == "" &&
		(u.Host == "wsboard.mts-link.ru" || strings.HasSuffix(u.Host, ".mts-link.ru")) && (u.Path == "" || u.Path == "/")
}

func (t *Transport) Start() error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	if t.IsRunning() {
		return nil
	}
	if !ValidURL(t.url) {
		return errors.New("mtslink: invalid public board URL")
	}
	if err := t.BaseTransport.Start(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.stopped = make(chan struct{})
	stopped := t.stopped
	utils.SafeGo("mtslink.connect", func() { defer close(stopped); t.connectLoop(ctx) })
	return nil
}
func (t *Transport) Stop() error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	if t.cancel != nil {
		t.cancel()
		if s := t.current.Load(); s != nil {
			s.conn.Close()
		}
		<-t.stopped
		t.cancel = nil
	}
	return t.BaseTransport.Stop()
}
func (t *Transport) Send(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > 256<<10 {
		return errors.New("mtslink: packet exceeds limit")
	}
	s := t.current.Load()
	if s == nil || !t.IsConnected() {
		return errors.New("mtslink: no authenticated board session")
	}
	select {
	case <-s.done:
		return errors.New("mtslink: session closed")
	default:
	}
	if err := s.write(fastView(s.info.ClientUID, s.uid, data)); err != nil {
		s.conn.Close()
		return errors.New("mtslink: write failed")
	}
	t.RecordSend(len(data))
	return nil
}
func (t *Transport) connectLoop(ctx context.Context) {
	attempt := 0
	for ctx.Err() == nil {
		began := time.Now()
		info, err := t.fetch(ctx)
		if err == nil && ctx.Err() == nil {
			err = t.serve(ctx, info)
		}
		t.SetConnected(false)
		if ctx.Err() != nil {
			return
		}
		utils.Debugf("[MTSLINK] connection ended; retrying") // never log URL/token/signature
		if time.Since(began) > time.Minute {
			attempt = 0
		}
		delay := 500 * time.Millisecond * time.Duration(1<<min(attempt, 5))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		attempt++
		t.RecordReconnect()
	}
}
func (t *Transport) serve(ctx context.Context, info baseInfo) error {
	conn, err := t.dial(ctx, info)
	if err != nil {
		return err
	}
	s := &session{conn: conn, info: info, done: make(chan struct{})}
	conn.SetReadLimit(maxFrame)
	// Stop must also interrupt the handshake, before a session is connected.
	t.current.Store(s)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-s.done:
		}
	}()
	var workerDone chan struct{}
	defer func() {
		close(s.done)
		conn.Close()
		if workerDone != nil {
			<-workerDone
		}
		t.current.CompareAndSwap(s, nil)
		t.SetConnected(false)
	}()
	if err = s.write(initMessage(info)); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, data, e := conn.ReadMessage()
		if e != nil {
			return e
		}
		var msg struct {
			Type       string          `json:"type"`
			SessionUID string          `json:"sessionUID"`
			Error      json.RawMessage `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type == "initResponse" {
			if msg.SessionUID == "" || (len(msg.Error) > 0 && string(msg.Error) != "null") {
				return errors.New("mtslink: guest access rejected")
			}
			s.uid = msg.SessionUID
			break
		}
		if msg.Type == "error" {
			return errors.New("mtslink: board init rejected")
		}
	}
	t.SetConnected(true)
	utils.Infof("[MTSLINK] board authorized")
	workerDone = make(chan struct{})
	go func() {
		defer close(workerDone)
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if s.write(map[string]string{"type": "pingRequest"}) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, data, e := conn.ReadMessage()
		if e != nil {
			return e
		}
		if p := packetPayload(data, info.ClientUID); len(p) > 0 {
			t.RecordReceive(len(p))
			t.CallReceive(p)
		}
	}
	return ctx.Err()
}
func (t *Transport) authorize(ctx context.Context) (baseInfo, error) {
	var zero baseInfo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DialContext: netbind.DialContext, Proxy: http.ProxyFromEnvironment},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 || !ValidURL(req.URL.String()) {
				return http.ErrUseLastResponse
			}
			return nil
		}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return zero, errors.New("mtslink: board request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return zero, fmt.Errorf("mtslink: board HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFrame+1))
	if err != nil || len(body) > maxFrame {
		return zero, errors.New("mtslink: board response exceeds limit")
	}
	return extractBaseInfo(body)
}
func (t *Transport) dialBoard(ctx context.Context, info baseInfo) (*websocket.Conn, error) {
	if !validWSDomain(info.WSDomain) {
		return nil, errors.New("mtslink: untrusted socket endpoint")
	}
	u := strings.TrimRight(info.WSDomain, "/") + "/ws/1?" + url.Values{"clientUID": {info.ClientUID}, "locale": {"en"}}.Encode()
	headers := http.Header{"User-Agent": {userAgent}, "Origin": {"https://my.mts-link.ru"}, "Cache-Control": {"no-cache"}, "Pragma": {"no-cache"}}
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: netbind.DialContext}
	conn, resp, err := d.DialContext(ctx, u, headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, errors.New("mtslink: WebSocket handshake failed")
	}
	return conn, nil
}

func extractBaseInfo(html []byte) (baseInfo, error) {
	var info baseInfo
	marker := []byte("const baseInfo = ")
	idx := bytes.Index(html, marker)
	if idx < 0 {
		return info, errors.New("mtslink: guest configuration missing; check board access")
	}
	tail := bytes.TrimSpace(html[idx+len(marker):])
	if len(tail) == 0 || tail[0] != '{' {
		return info, errors.New("mtslink: invalid guest configuration")
	}
	depth := 0
	quoted, escaped := false, false
	end := -1
	for i, c := range tail {
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if end < 0 {
		return info, errors.New("mtslink: truncated guest configuration")
	}
	// Quote only keys outside strings: strings can contain commas/colons/braces.
	raw := quoteJSKeys(tail[:end])
	if json.Unmarshal(raw, &info) != nil || info.BoardUID == "" || info.ClientUID == "" || !validWSDomain(info.WSDomain) {
		return info, errors.New("mtslink: unsupported guest configuration")
	}
	return info, nil
}
func quoteJSKeys(raw []byte) []byte {
	var out []byte
	quoted, escaped := false, false
	expect := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quoted {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			expect = false
			out = append(out, c)
			continue
		}
		if expect && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
			j := i + 1
			for j < len(raw) && ((raw[j] >= 'a' && raw[j] <= 'z') || (raw[j] >= 'A' && raw[j] <= 'Z') || (raw[j] >= '0' && raw[j] <= '9') || raw[j] == '_') {
				j++
			}
			out = append(out, '"')
			out = append(out, raw[i:j]...)
			out = append(out, '"')
			i = j - 1
			expect = false
			continue
		}
		if c == '{' || c == ',' {
			expect = true
		}
		if c == ':' {
			expect = false
		}
		out = append(out, c)
	}
	return out
}
func guestName() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "Guest" + hex.EncodeToString(b[:])
}
func initMessage(i baseInfo) map[string]any {
	return map[string]any{
		"type": "init", "board_uid": i.BoardUID, "token": i.ClientUID, "boardAccessToken": "", "jwt": "", "clientUID": i.ClientUID,
		"prefix": i.Prefix, "signature": i.Signature, "temporary": i.Temporary, "guestName": guestName()}
}
func fastView(token, uid string, p []byte) map[string]any {
	return map[string]any{"type": "fast", "subtype": "view", "data": map[string]any{
		"sessionUID": uid, "name": "Guest", "login": "", "token": token, "cursorPosition": map[string]any{"x": base64.StdEncoding.EncodeToString(p), "y": 350.0},
		"viewPosition": map[string]any{"viewportStartX": 0, "viewportStartY": 0, "viewportWidth": 1920, "viewportHeight": 1080}}}
}
func packetPayload(raw []byte, myToken string) []byte {
	var msg struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Data    struct {
			Token  string `json:"token"`
			Cursor struct {
				X json.RawMessage `json:"x"`
			} `json:"cursorPosition"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.Type != "fast" || msg.Subtype != "view" || msg.Data.Token == myToken {
		return nil
	}
	var x string
	if json.Unmarshal(msg.Data.Cursor.X, &x) != nil || len(x) > 400000 {
		return nil
	}
	p, e := base64.StdEncoding.DecodeString(x)
	if e != nil || len(p) > 256<<10 {
		return nil
	}
	return p
}
