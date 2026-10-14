//go:build android || ios

package desktop

import (
	"universal-bypass-tool/browser"
)

// Bridge is a compile-time stub. Android and iOS builds use browser/mobile
// instead; this file exists so the desktop package still type-checks in a
// single-repo build without pulling Playwright into the mobile binary.
type Bridge struct{}

func New(browser.Config) *Bridge { return &Bridge{} }

func (b *Bridge) Install() error                        { return browser.ErrUnsupported }
func (b *Bridge) Launch() error                         { return browser.ErrUnsupported }
func (b *Bridge) OpenTab(string) (browser.TabID, error) { return "", browser.ErrUnsupported }
func (b *Bridge) Evaluate(browser.TabID, string) (string, error) {
	return "", browser.ErrUnsupported
}
func (b *Bridge) Inbound(browser.TabID) <-chan string {
	closed := make(chan string)
	close(closed)
	return closed
}
func (b *Bridge) CloseTab(browser.TabID) error { return browser.ErrUnsupported }
func (b *Bridge) Close() error                 { return nil }

var _ browser.Bridge = (*Bridge)(nil)
