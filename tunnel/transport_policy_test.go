package tunnel

import (
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

type policyTransport struct {
	transport.Transport
	provider string
}

func (p policyTransport) TCPTransportProfile() string { return p.provider }

func TestBoardsPolicyIsProfileLocal(t *testing.T) {
	t.Setenv("PAPERFLUX_BOARDS_TCP_PRESET", "")
	for _, provider := range []string{"boards", "mailru", "yandex", "mtslink", ""} {
		p := &LazyProxy{upstream: policyTransport{provider: provider}}
		if got := boardsTCPPolicy(&lazyStackTransport{LazyProxy: p}); got != (provider == "boards") {
			t.Fatalf("policy leaked across lazy profile %q: %v", provider, got)
		}
	}
	t.Setenv("PAPERFLUX_BOARDS_TCP_PRESET", "legacy")
	if boardsTCPPolicy(policyTransport{provider: "boards"}) {
		t.Fatal("baseline override ignored")
	}
}

func TestBoardsTCPOptionsMatchUpstream(t *testing.T) {
	s := stack.New(stack.Options{TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocolCUBIC}})
	defer s.Destroy()
	if err := configureBoardsTCP(s); err != nil {
		t.Fatal(err)
	}
	var receive tcpip.TCPReceiveBufferSizeRangeOption
	var send tcpip.TCPSendBufferSizeRangeOption
	var rto tcpip.TCPMinRTOOption
	var recovery tcpip.TCPRecovery
	for _, option := range []tcpip.GettableTransportProtocolOption{&receive, &send, &rto, &recovery} {
		if err := s.TransportProtocolOption(tcp.ProtocolNumber, option); err != nil {
			t.Fatal(err)
		}
	}
	if receive.Default != 16<<20 || receive.Max != 64<<20 || send.Default != 16<<20 || send.Max != 64<<20 {
		t.Fatal("unexpected buffers", receive, send)
	}
	if time.Duration(rto) != 3*time.Second || recovery != 0 {
		t.Fatal("unexpected recovery", rto, recovery)
	}
}
