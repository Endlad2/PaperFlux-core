//go:build !android && !ios

package mobile

// Stub keeps the mobile package compiling on desktop without pulling in any
// mobile-only dependency. The real implementation is identical on desktop and
// mobile (it only needs the IPC client), so nothing is lost here.
//
// Intentionally empty: mobile.New is always available and desktop tests use
// it directly to exercise the IPC contract against a fake host.
