package tunnel

import (
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

type mailruPolicyWire struct {
	recoveryWire
	provider string
}

func (w *mailruPolicyWire) TCPTransportProfile() string { return w.provider }

func TestMailruExperimentIsExplicitAndProfileLocal(t *testing.T) {
	for _, mode := range []string{"", "legacy", "unknown", "packing", "cubic", "boost"} {
		t.Setenv("PAPERFLUX_MAILRU_TCP_PRESET", mode)
		for _, provider := range []string{"mailru", "boards", "yandex", "mtslink", ""} {
			want := ""
			if provider == "mailru" && (mode == "packing" || mode == "cubic" || mode == "boost") {
				want = mode
			}
			if got := mailruTCPExperiment(policyTransport{provider: provider}); got != want {
				t.Fatalf("%q/%q: %q != %q", provider, mode, got, want)
			}
		}
	}
}

func TestMailruStackTuningKeepsBufferBudget(t *testing.T) {
	t.Setenv("PAPERFLUX_TCP_BUFFER_PRESET", "")
	t.Setenv("PAPERFLUX_TCP_BUFFER_KIB", "")
	t.Setenv("PAPERFLUX_TCP_BUFFER_MAX_KIB", "")
	for _, mode := range []string{"legacy", "packing", "cubic", "boost"} {
		t.Setenv("PAPERFLUX_MAILRU_TCP_PRESET", mode)
		wire := &mailruPolicyWire{provider: "mailru"}
		tun := NewTCPTunnel(wire, false)
		var receive tcpip.TCPReceiveBufferSizeRangeOption
		var send tcpip.TCPSendBufferSizeRangeOption
		var rto tcpip.TCPMinRTOOption
		for _, option := range []tcpip.GettableTransportProtocolOption{&receive, &send, &rto} {
			if err := tun.gvisorStack.TransportProtocolOption(tcp.ProtocolNumber, option); err != nil {
				t.Fatal(err)
			}
		}
		if receive.Default != 256<<10 || send.Default != 256<<10 || receive.Max != 1<<20 || send.Max != 1<<20 {
			t.Fatal("buffer budget expanded")
		}
		wantMTU := uint32(1500)
		if mode == "packing" || mode == "boost" {
			wantMTU = 1400
		}
		if tun.tunnelEP.MTU() != wantMTU {
			t.Fatal("unexpected proxy MTU")
		}
		if (mode == "cubic" || mode == "boost") && time.Duration(rto) != 3*time.Second {
			t.Fatal("RTO floor not applied")
		}
		tun.Close()
	}
}

func TestTunnelEndpointMTUBounds(t *testing.T) {
	e := NewTunnelLinkEndpoint()
	if e.MTU() != 1500 {
		t.Fatal("legacy MTU changed")
	}
	e.SetMTU(1400)
	for _, value := range []uint32{0, 1000, 1501, 65000} {
		e.SetMTU(value)
	}
	if e.MTU() != 1400 {
		t.Fatal("invalid MTU accepted")
	}
}
