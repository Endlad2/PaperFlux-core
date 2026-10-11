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
