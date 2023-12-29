package api

import (
	"net"
)

type metrics interface {
	StreamOutbound(addr net.Addr)
	StreamOutboundError(addr net.Addr)
}
