package ipc

// Browser bridge message types. These extend the existing IPC protocol with
// out-of-band control for the WebView-based browser backend on Android/iOS.
//
// The host application (Java/Kotlin or Swift) is responsible for:
//   - creating one WebView per MsgBrowserOpenTab request,
//   - running JS on MsgBrowserEval and replying with MsgBrowserEvalReply,
//   - forwarding every inbound cursor frame via MsgBrowserInbound,
//   - writing one cursor line into the editor socket on MsgBrowserSend,
//   - closing the WebView on MsgBrowserCloseTab.
//
// All payloads are JSON; the frame itself stays a string so no escaping is
// introduced between Java/Swift and the document.
const (
	MsgBrowserOpenTab   MsgType = 0x10
	MsgBrowserEval      MsgType = 0x11
	MsgBrowserEvalReply MsgType = 0x12
	MsgBrowserInbound   MsgType = 0x13
	MsgBrowserCloseTab  MsgType = 0x14
	MsgBrowserTabOpened MsgType = 0x15
	MsgBrowserTabClosed MsgType = 0x16
	MsgBrowserSend      MsgType = 0x17
)

// BrowserOpenTabPayload asks the host to open one document tab.
type BrowserOpenTabPayload struct {
	TabID string `json:"tabId"`
	URL   string `json:"url"`
	// Headless is informational; the host may ignore it and always show the
	// tab, which is useful while debugging.
	Headless bool `json:"headless,omitempty"`
}

// BrowserEvalPayload asks the host to run JS in one tab.
type BrowserEvalPayload struct {
	CorrelationID string `json:"correlationId"`
	TabID         string `json:"tabId"`
	Script        string `json:"script"`
}

// BrowserEvalReplyPayload carries the string result of BrowserEvalPayload.
type BrowserEvalReplyPayload struct {
	CorrelationID string `json:"correlationId"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
}

// BrowserInboundPayload carries one cursor frame from the host to Go.
type BrowserInboundPayload struct {
	TabID string `json:"tabId"`
	Frame string `json:"frame"`
}

// BrowserSendPayload asks the host to write one cursor line into the editor
// socket of the given tab. The line is already the exact wire format:
//
//	42["message",{"type":"cursor","cursor":"<len>;<base64>"}]
type BrowserSendPayload struct {
	TabID string `json:"tabId"`
	Line  string `json:"line"`
}

// BrowserCloseTabPayload asks the host to close one tab.
type BrowserCloseTabPayload struct {
	TabID string `json:"tabId"`
}
