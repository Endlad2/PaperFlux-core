package mobile

import "universal-bypass-tool/browser"

func init() {
	// On mobile, "installing" means the host application must be reachable.
	// There is nothing to download; the WebView is system-provided.
	browser.SetInstallFunc(func() error { return nil })
}
