// Package browser provides an alternative transport backend that drives a real
// browser (Playwright on desktop, WebView on mobile) instead of speaking to
// the document provider's HTTP/WebSocket API directly.
package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"universal-bypass-tool/transport"
)

// TabID identifies one open document tab inside the browser backend.
type TabID string

// Bridge is the platform-specific browser control plane.
type Bridge interface {
	Install() error
	Launch() error
	OpenTab(url string) (TabID, error)

	// Send writes one cursor line to the editor's WebSocket. The line is the
	// exact wire format the raw transports emit, e.g.
	//   42["message",{"type":"cursor","cursor":"N;<base64>"}]
	Send(tab TabID, line string) error

	// Inbound returns cursor frames received from the editor, already in the
	// same wire format as Send.
	Inbound(tab TabID) <-chan string

	CloseTab(tab TabID) error
	Close() error
}

// Config controls how the browser backend talks to the provider.
type Config struct {
	Headless      bool
	UserAgent     string
	OpenTimeout   time.Duration
	InboundBuffer int
	ProfileDir    string
}

// DefaultConfig returns the development browser backend defaults.
func DefaultConfig() Config {
	return Config{
		Headless:      false,
		OpenTimeout:   90 * time.Second,
		InboundBuffer: 4096,
		ProfileDir:    DefaultProfileDir(),
	}
}

// DefaultProfileDir returns the per-user directory where the browser keeps
// its persistent profile.
func DefaultProfileDir() string {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("APPDATA")
		}
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "PaperFlux", "browser-profile")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "PaperFlux", "browser-profile")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "paperflux", "browser-profile")
	}
}

var (
	ErrBrowserNotInstalled = errors.New("browser: runtime not installed")
	ErrTabClosed           = errors.New("browser: tab closed")
	ErrUnsupported         = errors.New("browser: backend not available on this build")
)

// newTransportFn is injected by session_runtime.go.
var newTransportFn func(cfg Config, provider string, urls []string, identity Identity) (transport.Transport, error)

// Identity mirrors the fields the raw transports need.
type Identity struct {
	ID        string
	Token     string
	CookieDir string
	Exit      bool
}

// RegisterTransportFactory installs the function that builds the concrete
// transport wrapping this bridge.
func RegisterTransportFactory(fn func(cfg Config, provider string, urls []string, identity Identity) (transport.Transport, error)) {
	if newTransportFn != nil {
		panic("browser: transport factory already registered")
	}
	newTransportFn = fn
}

// New builds the browser-backed transport for one profile lane.
func New(cfg Config, provider string, urls []string, identity Identity) (transport.Transport, error) {
	if newTransportFn == nil {
		return nil, fmt.Errorf("browser: transport factory not registered")
	}
	if provider == "" {
		return nil, fmt.Errorf("browser: provider is required")
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("browser: at least one document URL is required")
	}
	if cfg.ProfileDir == "" {
		cfg.ProfileDir = DefaultProfileDir()
	}
	return newTransportFn(cfg, provider, urls, identity)
}

// IsCursorSocket reports whether a WebSocket URL is the editor's cursor
// channel. This is the socket that carries the 42["message",{"type":"cursor"}]
// frames PaperFlux tunnels data through. Every other WebSocket the page opens
// (telemetry, push notifications, analytics) is ignored.
func IsCursorSocket(rawURL string) bool {
	// The path segment is the only reliable marker: it is the ONLYOFFICE
	// cursor endpoint on every provider that still exposes one.
	if !contains(rawURL, "/doc/") {
		return false
	}
	// Engine.IO handshake is required: a plain /doc/ HTTP request is not the
	// socket PaperFlux tunnels through.
	return contains(rawURL, "EIO=4") || contains(rawURL, "transport=websocket")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
