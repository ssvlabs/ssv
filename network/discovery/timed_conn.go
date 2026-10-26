package discovery

import (
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

// TimedConn wraps a discv5 UDP socket and records when it was last read from.
//
// A discv5 listener drains its socket only through ReadFromUDPAddrPort, so a
// wedged socket — bound but no longer read, leaving discovery silently dead —
// shows up as a last-read timestamp that stops advancing. StaleFor turns that
// into a liveness signal for both the operator and boot nodes.
//
// The operator runs two discv5 listeners on one socket, named after the
// protocol-ID fork that split them — historical names, as both run permanently
// (see forkingDV5Listener). Only the post-fork one (SSV protocol ID) reads the
// socket, through this wrapper; the pre-fork one (go-ethereum's default ID)
// reads the packets the post-fork one couldn't decode, relayed via
// SharedUDPConn. A stall confined to the pre-fork listener is therefore
// invisible here, and can't back up into the socket reader: the relay drops
// rather than blocks (#2980). The boot node's single listener reads the socket
// directly.
//
// Only ReadFromUDPAddrPort is overridden; writes, Close and LocalAddr fall
// through to the embedded conn.
type TimedConn struct {
	discover.UDPConn

	lastReadUnixNano atomic.Int64
	read             atomic.Bool      // set by the first successful read
	now              func() time.Time // overridable in tests; nil means time.Now
}

var _ discover.UDPConn = (*TimedConn)(nil)

// NewTimedConn wraps conn, seeding the last-read time so LastRead is meaningful
// before the first read. Staleness arms only on a read (see StaleFor).
func NewTimedConn(conn *net.UDPConn) *TimedConn {
	c := &TimedConn{UDPConn: conn}
	c.lastReadUnixNano.Store(c.nowFn().UnixNano())
	return c
}

func (c *TimedConn) nowFn() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// ReadFromUDPAddrPort delegates, stamping the read time on success. Errors don't
// stamp, so a closed or failing socket can't look freshly drained.
func (c *TimedConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	n, addr, err = c.UDPConn.ReadFromUDPAddrPort(b)
	if err == nil {
		c.lastReadUnixNano.Store(c.nowFn().UnixNano())
		c.read.Store(true)
	}
	return n, addr, err
}

// LastRead reports when a packet was last read from the socket.
func (c *TimedConn) LastRead() time.Time {
	return time.Unix(0, c.lastReadUnixNano.Load())
}

// ReadStaleness reports how long the socket has gone unread; ok is false until
// the first successful read, before which there is nothing to measure.
func (c *TimedConn) ReadStaleness() (age time.Duration, ok bool) {
	if !c.read.Load() {
		return 0, false
	}
	return c.nowFn().Sub(c.LastRead()), true
}

// StaleFor reports whether the socket has wedged: read at least once, then
// unread for longer than d. A never-read socket is never stale: a wedge can only
// arise once draining has started, and a node with no inbound discv5 traffic yet
// (unreachable bootnodes, blocked UDP) is not something a restart can fix.
func (c *TimedConn) StaleFor(d time.Duration) bool {
	age, ok := c.ReadStaleness()
	return ok && age > d
}
