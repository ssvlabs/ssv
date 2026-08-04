package discovery

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

const (
	// unhandledChanSize is the handoff capacity between go-ethereum's forwarding
	// send and drain. It stays small because drain is always receptive; bursts
	// are absorbed by unhandledBufferSize instead.
	// Size taken from https://github.com/ethereum/go-ethereum/blob/v1.13.5/p2p/server.go#L551
	unhandledChanSize = 100

	// unhandledBufferSize bounds how many forwarded packets are held for the
	// pre-fork listener before drain starts discarding them.
	unhandledBufferSize = 1024
)

// SharedUDPConn implements a shared connection: writes go to the underlying
// socket, and reads return the packets the primary listener could not decode
// and forwarded to Unhandled, minus any dropped under load.
// Originally copied from https://github.com/ethereum/go-ethereum/blob/v1.14.8/p2p/server.go#L435
//
// Unlike upstream, reads are served from an internal buffer filled by drain,
// not from Unhandled directly. That decoupling is load-bearing: go-ethereum
// forwards undecodable packets with a blocking send and no default case
// (v5_udp.go, handlePacket) on the post-fork listener's dispatch goroutine,
// which re-arms its socket read only after handlePacket returns. Serving reads
// straight off Unhandled — one packet per dispatch cycle — let a burst of junk
// (scan noise, stray discv4, a flood) stall that listener until it stopped
// draining the socket, and the kernel dropped valid discv5 traffic. drain
// always accepts, dropping and counting on overflow instead.
type SharedUDPConn struct {
	*net.UDPConn
	Unhandled chan discover.ReadPacket

	buffered   chan discover.ReadPacket
	readerDone chan struct{}
	closeOnce  sync.Once
	drained    sync.WaitGroup
	dropped    atomic.Uint64
}

// NewSharedUDPConn returns a SharedUDPConn and starts draining unhandled.
//
// Ownership of unhandled stays with the caller: drain runs until unhandled is
// closed, which must not happen until the listener producing into it has fully
// stopped. Closing it earlier panics that producer mid-send.
func NewSharedUDPConn(ctx context.Context, conn *net.UDPConn, unhandled chan discover.ReadPacket) *SharedUDPConn {
	s := &SharedUDPConn{
		UDPConn:    conn,
		Unhandled:  unhandled,
		buffered:   make(chan discover.ReadPacket, unhandledBufferSize),
		readerDone: make(chan struct{}),
	}
	s.drained.Add(1)
	go s.drain(ctx)
	return s
}

// drain moves packets from Unhandled into the internal buffer, discarding them
// when it is full. It stays receptive for the producer's whole lifetime, so
// go-ethereum's forwarding send never blocks longer than the handoff.
//
// ctx carries the metric context only — it is deliberately not a stop signal.
// Draining must outlive cancellation: DiscV5Service.Close cancels before it
// joins the producer, so stopping on ctx.Done here would let the post-fork
// listener wedge on a full channel or panic sending to a closed one. drain
// stops only when Unhandled is closed, which its owner does after that join.
func (s *SharedUDPConn) drain(ctx context.Context) {
	defer s.drained.Done()
	for packet := range s.Unhandled {
		select {
		case s.buffered <- packet:
		default:
			s.dropped.Add(1)
			recordUnhandledPacketDropped(ctx)
		}
	}
}

// Dropped reports how many forwarded packets were discarded due to a full buffer.
func (s *SharedUDPConn) Dropped() uint64 {
	return s.dropped.Load()
}

// ReadFromUDPAddrPort implements discover.UDPConn
func (s *SharedUDPConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	// Check for close on its own first: a single select over both cases picks
	// randomly when both are ready, so a buffer that stays non-empty could
	// starve the close signal and leave go-ethereum's readLoop running.
	select {
	case <-s.readerDone:
		return 0, netip.AddrPort{}, net.ErrClosed
	default:
	}

	select {
	case <-s.readerDone:
		return 0, netip.AddrPort{}, net.ErrClosed
	case packet := <-s.buffered:
		l := min(len(packet.Data), len(b))
		copy(b[:l], packet.Data[:l])
		return l, packet.Addr, nil
	}
}

// Close implements discover.UDPConn.
//
// It releases the reader without touching the underlying socket, which is owned
// by the post-fork listener, and without closing Unhandled, which that listener
// is still writing to. Draining continues so the producer cannot wedge while it
// shuts down.
func (s *SharedUDPConn) Close() error {
	s.closeOnce.Do(func() {
		close(s.readerDone)
	})
	return nil
}

// WaitDrained blocks until drain has exited, which happens once the owner of
// Unhandled closes it.
func (s *SharedUDPConn) WaitDrained() {
	s.drained.Wait()
}
