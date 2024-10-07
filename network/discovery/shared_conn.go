package discovery

import (
	"net"
	"net/netip"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/pkg/errors"
)

// UDPConn is a network connection on which discovery can operate.
type UDPConn interface {
	ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error)
	WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

// SharedUDPConn implements a shared connection. Write sends messages to the underlying connection while read returns
// messages that were found unprocessable and sent to the unhandled channel by the primary listener.
// It's copied from https://github.com/ethereum/go-ethereum/blob/v1.14.8/p2p/server.go#L435
type SharedUDPConn struct {
	UDPConn   *net.UDPConn // not using embedding to make sure go-ethereum doesn't use unwrapped net.UDPConn's methods
	Unhandled chan discover.ReadPacket
}

// ReadFromUDPAddrPort implements discover.UDPConn
func (s *SharedUDPConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	packet, ok := <-s.Unhandled
	if !ok {
		return 0, netip.AddrPort{}, errors.New("connection was closed")
	}
	l := len(packet.Data)
	if l > len(b) {
		l = len(b)
	}
	copy(b[:l], packet.Data[:l])
	return l, packet.Addr, nil
}

func (s *SharedUDPConn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (n int, err error) {
	return s.UDPConn.WriteToUDPAddrPort(b, addr)
}

func (s *SharedUDPConn) LocalAddr() net.Addr {
	return s.UDPConn.LocalAddr()
}

// Close implements discover.UDPConn
func (s *SharedUDPConn) Close() error {
	return nil
}
