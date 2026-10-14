//go:build !android && !ios

package desktop

import (
	"github.com/mxschmitt/playwright-go"
	"universal-bypass-tool/browser"
)

func init() {
	browser.SetInstallFunc(func() error {
		return playwright.Install()
	})
}
