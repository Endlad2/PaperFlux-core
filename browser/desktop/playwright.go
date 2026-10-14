//go:build !android && !ios

// Package desktop implements the browser.Bridge on top of Playwright for Go.
// It is compiled only for desktop targets; Android and iOS use the mobile
// bridge instead.
//
// WebSocket discovery uses Playwright's own WebSocketRoute, which intercepts
// every socket of the browser context, including the ones opened inside a
// cross-origin iframe. Mail.ru serves its editor from docs.datacloudmail.ru
// inside an iframe on cloud.mail.ru, and a JavaScript hook cannot reach that
// socket reliably: the iframe creates it before any frameattached handler can
// run in it, and a cross-origin iframe cannot call a function on the parent.
// WebSocketRoute has neither limitation, so there is no JS hook here at all.
package desktop

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"universal-bypass-tool/browser"
)

// DesktopHeadless is the shipped default when browser.Config.Headless is left
// at its zero value.
const DesktopHeadless = True

// debugLog mirrors the -debug flag.
var debugLog bool

// SetDebug enables per-frame logging. Called from session_runtime.go when the
// process is running with -debug.
func SetDebug(enabled bool) { debugLog = enabled }

// Bridge owns one Playwright instance and one persistent browser context for
// the whole process.
type Bridge struct {
	cfg browser.Config

	launchMu sync.Mutex

	mu      sync.Mutex
	pw      *playwright.Playwright
	context playwright.BrowserContext

	tabs map[browser.TabID]*tab
	// pendingTab is the tab whose page is currently navigating. The route
	// handler fires before OpenTab can register the tab in b.tabs (the page
	// must exist and be navigated before its socket opens), so the handler
	// resolves its owner through this field instead.
	pendingTab *tab
	next       int64

	closed bool
}

// tab is one document page. The cursor socket is captured as a pair of
// WebSocketRoute objects: pageRoute speaks to the editor, serverRoute speaks
// to the provider. Sending a tunnel frame goes through serverRoute, as if it
// arrived from the provider; receiving a tunnel frame comes from serverRoute
// too, as if the provider sent it.
type tab struct {
	id      browser.TabID
	page    playwright.Page
	inbound chan string

	socketMu    sync.Mutex
	pageRoute   playwright.WebSocketRoute
	serverRoute playwright.WebSocketRoute
	url         string

	closeOnce sync.Once
}

var (
	sharedMu     sync.Mutex
	sharedBridge *Bridge
)

// GetOrCreate returns the process-wide desktop bridge, creating it on first
// call. One Chromium owns one profile directory, so every document lane must
// reuse the same Bridge.
func GetOrCreate(cfg browser.Config) *Bridge {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedBridge != nil {
		sharedBridge.mu.Lock()
		closed := sharedBridge.closed
		sharedBridge.mu.Unlock()
		if !closed {
			return sharedBridge
		}
	}
	sharedBridge = New(cfg)
	return sharedBridge
}

// Shutdown closes the process-wide bridge.
func Shutdown() error {
	sharedMu.Lock()
	b := sharedBridge
	sharedBridge = nil
	sharedMu.Unlock()
	if b == nil {
		return nil
	}
	return b.Close()
}

// New returns an unstarted desktop bridge.
func New(cfg browser.Config) *Bridge {
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 90 * time.Second
	}
	if cfg.InboundBuffer <= 0 {
		cfg.InboundBuffer = 4096
	}
	if cfg.ProfileDir == "" {
		cfg.ProfileDir = browser.DefaultProfileDir()
	}
	return &Bridge{cfg: cfg, tabs: make(map[browser.TabID]*tab)}
}

// Install downloads the Playwright driver and browsers if they are missing.
func (b *Bridge) Install() error {
	log.Printf("[BROWSER] installing Playwright driver and browsers (first run may take a minute)")
	return playwright.Install()
}

// Launch starts a persistent Chromium context and installs the WebSocket
// route that discovers cursor sockets. The route must be installed before
// any page is created, otherwise Playwright will not intercept sockets opened
// during the first navigation.
func (b *Bridge) Launch() error {
	b.launchMu.Lock()
	defer b.launchMu.Unlock()

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("desktop: bridge closed")
	}
	if b.context != nil {
		b.mu.Unlock()
		return nil
	}
	cfg := b.cfg
	b.mu.Unlock()

	headless := cfg.Headless
	if !headless && DesktopHeadless {
		headless = true
	}

	profileDir := cfg.ProfileDir
	if profileDir == "" {
		profileDir = browser.DefaultProfileDir()
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return fmt.Errorf("desktop: profile dir %s: %w", profileDir, err)
	}
	log.Printf("[BROWSER] launching persistent Chromium headless=%t profile=%s", headless, profileDir)

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("desktop: playwright run: %w", err)
	}

	opts := playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(headless),
		Args: []string{
			"--disable-gpu",
			"--no-sandbox",
			"--disable-dev-shm-usage",
		},
	}
	if cfg.UserAgent != "" {
		opts.UserAgent = playwright.String(cfg.UserAgent)
	}
	context, err := pw.Chromium.LaunchPersistentContext(profileDir, opts)
	if err != nil {
		_ = pw.Stop()
		return fmt.Errorf("desktop: launch persistent chromium: %w", err)
	}

	// Intercept every WebSocket of the context. The URL pattern is broad on
	// purpose: the cursor socket can be on any host (docs.datacloudmail.ru,
	// docs.yandex.ru, ...) and the filter is applied in the handler, not in
	// the pattern. Playwright applies the route before the socket is
	// actually connected, which is what makes cross-origin iframe sockets
	// reachable here.
	if err := context.RouteWebSocket("**/*", func(route playwright.WebSocketRoute) {
		wsURL := route.URL()
		if debugLog {
			log.Printf("[WS-ROUTE] url=%s", wsURL)
		}
		if !browser.IsCursorSocket(wsURL) {
			// Not the cursor channel: pass it through untouched, so
			// analytics, push and telemetry keep working normally.
			if _, err := route.ConnectToServer(); err != nil && debugLog {
				log.Printf("[WS-ROUTE] passthrough connect failed url=%s err=%v", wsURL, err)
			}
			return
		}
		b.attachCursorSocket(route, wsURL)
	}); err != nil {
		_ = context.Close()
		_ = pw.Stop()
		return fmt.Errorf("desktop: RouteWebSocket: %w", err)
	}

	b.mu.Lock()
	b.pw, b.context = pw, context
	b.mu.Unlock()

	log.Printf("[BROWSER] Chromium ready (profile persisted at %s)", profileDir)
	return nil
}

// attachCursorSocket binds one cursor WebSocketRoute to the tab that owns it.
// The route handler runs on the Playwright driver goroutine, so it must stay
// short: it only wires callbacks and stores the routes, it does not block.
func (b *Bridge) attachCursorSocket(pageRoute playwright.WebSocketRoute, wsURL string) {
	serverRoute, err := pageRoute.ConnectToServer()
	if err != nil {
		log.Printf("[BROWSER] cursor socket connect failed url=%s err=%v", wsURL, err)
		return
	}
	log.Printf("[BROWSER] cursor socket attached url=%s", wsURL)

	b.mu.Lock()
	owner := b.pendingTab
	if owner == nil {
		// Fall back to any registered tab; the route can fire between the
		// navigation commit and the moment OpenTab registers the tab.
		for _, t := range b.tabs {
			owner = t
			break
		}
	}
	b.mu.Unlock()
	if owner == nil {
		return
	}

	owner.socketMu.Lock()
	owner.pageRoute = pageRoute
	owner.serverRoute = serverRoute
	owner.url = wsURL
	owner.socketMu.Unlock()

	// Forward page -> server. Calling OnMessage disables Playwright's own
	// forwarding on this side, so the handler must re-send explicitly.
	pageRoute.OnMessage(func(msg any) {
		if debugLog {
			log.Printf("[WS-ROUTE] page->server len=%d payload=%s", payloadLen(msg), truncate(payloadString(msg), 200))
		}
		serverRoute.Send(msg)
	})

	// Forward server -> page and capture cursor frames. This is the side that
	// carries the tunnel data on the way in.
	serverRoute.OnMessage(func(msg any) {
		text := payloadString(msg)
		if debugLog {
			log.Printf("[WS-ROUTE] server->page len=%d payload=%s", payloadLen(msg), truncate(text, 200))
		}
		pageRoute.Send(msg)
		if !looksLikeCursorLine(text) {
			return
		}
		select {
		case owner.inbound <- text:
		default:
		}
	})

	// Preserve close propagation both ways, because installing OnMessage on
	// either side also disables the automatic close forwarding for that side.
	pageRoute.OnClose(func(code *int, reason *string) {
		serverRoute.Close()
		if debugLog {
			log.Printf("[WS-ROUTE] page closed url=%s", wsURL)
		}
	})
	serverRoute.OnClose(func(code *int, reason *string) {
		pageRoute.Close()
		if debugLog {
			log.Printf("[WS-ROUTE] server closed url=%s", wsURL)
		}
	})
}

// OpenTab navigates a fresh page to url and waits until the cursor socket is
// attached.
func (b *Bridge) OpenTab(url string) (browser.TabID, error) {
	b.mu.Lock()
	if b.closed || b.context == nil {
		b.mu.Unlock()
		return "", fmt.Errorf("desktop: not launched")
	}
	b.next++
	id := browser.TabID(fmt.Sprintf("desktop-%d", b.next))
	context := b.context
	b.mu.Unlock()

	log.Printf("[BROWSER] opening tab %s -> %s", id, url)
	page, err := context.NewPage()
	if err != nil {
		return "", fmt.Errorf("desktop: new page: %w", err)
	}

	inbound := make(chan string, b.cfg.InboundBuffer)
	t := &tab{id: id, page: page, inbound: inbound}

	// Register the pending tab before navigation so the route handler can
	// find its owner when the cursor socket opens mid-load.
	b.mu.Lock()
	b.pendingTab = t
	b.mu.Unlock()

	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		_ = page.Close()
		return "", fmt.Errorf("desktop: goto %s: %w", url, err)
	}
	log.Printf("[BROWSER] tab %s: document loaded, waiting for editor", id)

	if err := b.waitForEditor(t); err != nil {
		_ = page.Close()
		return "", err
	}
	log.Printf("[BROWSER] tab %s ready", id)

	b.mu.Lock()
	b.pendingTab = nil
	b.tabs[id] = t
	b.mu.Unlock()
	return id, nil
}

// waitForEditor polls until the cursor socket was attached.
func (b *Bridge) waitForEditor(t *tab) error {
	deadline := time.Now().Add(b.cfg.OpenTimeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	reported := false
	lastURL := ""
	for range tick.C {
		if time.Now().After(deadline) {
			return fmt.Errorf("desktop: tab %s editor not ready after %s", t.id, b.cfg.OpenTimeout)
		}
		t.socketMu.Lock()
		ready := t.serverRoute != nil
		t.socketMu.Unlock()
		if ready {
			return nil
		}
		if current := t.page.URL(); current != lastURL {
			lastURL = current
			log.Printf("[BROWSER] tab %s: URL is now %s", t.id, current)
		}
		if !reported {
			log.Printf("[BROWSER] tab %s: waiting for cursor socket on %s", t.id, t.page.URL())
			reported = true
		}
	}
	return fmt.Errorf("desktop: tab %s editor not ready after %s", t.id, b.cfg.OpenTimeout)
}

// Send writes one cursor line to the editor's socket, as if the provider had
// sent it. The line is the exact wire format the raw transports use.
func (b *Bridge) Send(id browser.TabID, line string) error {
	t, err := b.tab(id)
	if err != nil {
		return err
	}
	t.socketMu.Lock()
	serverRoute := t.serverRoute
	t.socketMu.Unlock()
	if serverRoute == nil {
		return fmt.Errorf("desktop: cursor socket is not open")
	}
	if debugLog {
		log.Printf("[BROWSER] tab %s: send len=%d payload=%s", id, len(line), truncate(line, 200))
	}
	serverRoute.Send(line)
	return nil
}

func (b *Bridge) Inbound(id browser.TabID) <-chan string {
	b.mu.Lock()
	t := b.tabs[id]
	if t == nil {
		t = b.pendingTab
	}
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
	var err error
	t.closeOnce.Do(func() {
		close(t.inbound)
		t.socketMu.Lock()
		serverRoute, pageRoute := t.serverRoute, t.pageRoute
		t.serverRoute, t.pageRoute = nil, nil
		t.socketMu.Unlock()
		if serverRoute != nil {
			serverRoute.Close()
		}
		if pageRoute != nil {
			pageRoute.Close()
		}
		err = t.page.Close()
	})
	return err
}

func (b *Bridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	tabs := make([]*tab, 0, len(b.tabs))
	for _, t := range b.tabs {
		tabs = append(tabs, t)
	}
	b.tabs = make(map[browser.TabID]*tab)
	b.pendingTab = nil
	context, pw := b.context, b.pw
	b.context, b.pw = nil, nil
	b.mu.Unlock()

	for _, t := range tabs {
		t.closeOnce.Do(func() {
			close(t.inbound)
			t.socketMu.Lock()
			serverRoute, pageRoute := t.serverRoute, t.pageRoute
			t.serverRoute, t.pageRoute = nil, nil
			t.socketMu.Unlock()
			if serverRoute != nil {
				serverRoute.Close()
			}
			if pageRoute != nil {
				pageRoute.Close()
			}
			_ = t.page.Close()
		})
	}
	if context != nil {
		_ = context.Close()
	}
	if pw != nil {
		_ = pw.Stop()
	}
	return nil
}

func (b *Bridge) tab(id browser.TabID) (*tab, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, browser.ErrTabClosed
	}
	t := b.tabs[id]
	if t == nil {
		return nil, browser.ErrTabClosed
	}
	return t, nil
}

// looksLikeCursorLine is a fast pre-filter on inbound frames.
func looksLikeCursorLine(text string) bool {
	if len(text) < 12 {
		return false
	}
	return contains(text, `"type":"cursor"`) && contains(text, `"cursor"`)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// payloadString converts a WebSocketRoute message into its textual form.
// Playwright hands the payload back as a string for text frames.
func payloadString(msg any) string {
	switch v := msg.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func payloadLen(msg any) int {
	switch v := msg.(type) {
	case string:
		return len(v)
	case []byte:
		return len(v)
	default:
		return 0
	}
}

// truncate keeps traces readable and avoids dumping multi-megabyte document
// history or full base64 payloads into the console.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("...(total %d)", len(s))
}

var _ browser.Bridge = (*Bridge)(nil)
