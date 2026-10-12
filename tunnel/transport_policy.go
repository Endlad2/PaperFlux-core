package tunnel

import (
	"fmt"
	"os"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"universal-bypass-tool/transport"
)

// The hint belongs to a profile, not the process: a mixed exit group must not
// change Mail.ru/Docs when Boards opts into upstream's TCP parameters.
type tcpTransportProfile interface{ TCPTransportProfile() string }

// Opt-in owner performance experiment, profile-local. Keep bounded buffers
// and the Mail.ru cursor budget unchanged. Empty/legacy keeps deployed TCP.
func mailruTCPExperiment(trans transport.Transport) string {
	hint, ok := trans.(tcpTransportProfile)
	if !ok || hint.TCPTransportProfile() != "mailru" {
		return ""
	}
	switch mode := os.Getenv("PAPERFLUX_MAILRU_TCP_PRESET"); mode {
	case "packing", "cubic", "boost":
		return mode
	default:
		return ""
	}
}

func configureMailruTCP(s *stack.Stack) error {
	if err := configureTCPRecovery(s, "classic"); err != nil {
		return err
	}
	rto := tcpip.TCPMinRTOOption(3 * time.Second)
	if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, &rto); err != nil {
		return fmt.Errorf("set Mail.ru minimum RTO: %s", err)
	}
	return nil
}

func boardsTCPPolicy(trans transport.Transport) bool {
	hint, ok := trans.(tcpTransportProfile)
	return ok && hint.TCPTransportProfile() == "boards" && os.Getenv("PAPERFLUX_BOARDS_TCP_PRESET") != "legacy"
}

func configureBoardsTCP(s *stack.Stack) error {
	if err := configureTCPRecovery(s, "classic"); err != nil {
		return err
	}
	rto := tcpip.TCPMinRTOOption(3 * time.Second)
	if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, &rto); err != nil {
		return fmt.Errorf("set Boards minimum RTO: %s", err)
	}
	if err := configureTCPBufferRange(s, 4<<20, 16<<20, 64<<20); err != nil {
		return fmt.Errorf("set Boards buffers: %s", err)
	}
	return nil
}

func (p *LazyProxy) TCPTransportProfile() string {
	if hint, ok := p.upstream.(tcpTransportProfile); ok {
		return hint.TCPTransportProfile()
	}
	return ""
}
