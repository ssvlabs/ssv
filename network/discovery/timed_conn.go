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
// into a liveness signal, used by both the operator and boot nodes.
//
// Only ReadFromUDPAddrPort is overridden; writes, Close and LocalAddr fall
// through to the embedded conn.
type TimedConn struct {
	discover.UDPConn

	lastReadUnixNano atomic.Int64
	read             atomic.Bool      // set once the socket has been read at least once
	now              func() time.Time // overridable in tests; nil means time.Now
}

var _ discover.UDPConn = (*TimedConn)(nil)

// NewTimedConn wraps conn, seeding the last-read time so LastRead reports a
// sensible value before the first read. Staleness only arms once the socket has
// actually been read (see StaleFor).
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

// ReadFromUDPAddrPort delegates and, on success, records the read time and marks
// the socket as having been read. Errors leave both untouched, so a closed or
// failing socket can't look freshly drained.
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

// ReadStaleness reports how long the socket has gone unread, and whether it has
// ever been read. Before the first successful read there is nothing to measure,
// so ok is false: a socket that has never delivered a packet is not wedged, it
// has simply had no inbound discv5 traffic yet (unreachable bootnodes, blocked
// UDP), which a restart cannot fix.
func (c *TimedConn) ReadStaleness() (age time.Duration, ok bool) {
	if !c.read.Load() {
		return 0, false
	}
	return c.nowFn().Sub(c.LastRead()), true
}

// StaleFor reports whether the socket has wedged: it was being drained, then
// went unread for longer than d. A socket that has never been read is never
// stale — a wedge can only arise after draining has started, so requiring a
// prior read rules out the false positive (a healthy node that has simply had
// no inbound traffic yet) without missing any real wedge.
func (c *TimedConn) StaleFor(d time.Duration) bool {
	age, ok := c.ReadStaleness()
	return ok && age > d
}
