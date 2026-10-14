package browser

import (
	"universal-bypass-tool/transport"
)

// WrapBridge is the public constructor used by the transport factory in
// session_runtime.go. It is separate from New so the factory can inject a
// concrete Bridge (desktop or mobile) without going through the platform
// dispatch inside New.
func WrapBridge(bridge Bridge, cfg Config, provider string, urls []string, identity Identity) transport.Transport {
	return newBackend(bridge, cfg, provider, urls, identity)
}

// installRuntime is set by the platform bridge at init time. It exists so a
// caller can make sure the browser runtime is present before launching any
// tab, without knowing whether the build is desktop or mobile.
var installRuntime = func() error { return nil }

// SetInstallFunc is called by desktop/mobile at init to make Install work
// before any Bridge instance exists.
func SetInstallFunc(fn func() error) {
	if fn != nil {
		installRuntime = fn
	}
}

// Install prepares the browser runtime for the current platform. On desktop
// this downloads the Playwright driver and browsers; on mobile it is a no-op
// because the WebView is provided by the host application.
func Install() error { return installRuntime() }
