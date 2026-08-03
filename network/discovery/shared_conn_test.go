package discovery

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/stretchr/testify/require"
)

func testPacket(b byte) discover.ReadPacket {
	return discover.ReadPacket{
		Data: []byte{b},
		Addr: netip.MustParseAddrPort("1.2.3.4:30303"),
	}
}

// forwardBlocking mimics how go-ethereum hands undecodable packets to the
// pre-fork listener: a bare blocking send with no default case and no context
// check (p2p/discover/v5_udp.go, handlePacket). It runs on the post-fork
// listener's dispatch goroutine, so anything that blocks here stops that
// listener from reading the real UDP socket.
func forwardBlocking(unhandled chan discover.ReadPacket, n int, sent *atomic.Int64) {
	for i := 0; i < n; i++ {
		unhandled <- testPacket(byte(i))
		sent.Add(1)
	}
}

// TestSharedUDPConn_ForwardingNeverBlocks is the regression test for the wedge:
// with no reader consuming at all, a producer must still be able to forward far
// more packets than any buffer holds. Tying Unhandled directly to the pre-fork
// listener's read path made this block once ~100 packets were in flight, which
// halted the post-fork listener and stopped the UDP socket being drained.
func TestSharedUDPConn_ForwardingNeverBlocks(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, 100)
	conn := NewSharedUDPConn(context.Background(), nil, unhandled)

	// Well past unhandled's capacity and the internal buffer combined.
	const total = 100 + unhandledBufferSize + 5000

	var sent atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardBlocking(unhandled, total, &sent)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("forwarding blocked after %d/%d packets: the post-fork listener "+
			"would be stalled here and the UDP socket left undrained", sent.Load(), total)
	}

	require.EqualValues(t, total, sent.Load())
	require.Positive(t, conn.Dropped(), "overflow should be dropped and counted, not blocked on")

	close(unhandled)
	conn.WaitDrained()
}

// TestSharedUDPConn_DeliversPacketsToReader covers the normal path: what the
// producer forwards is what the pre-fork listener reads.
func TestSharedUDPConn_DeliversPacketsToReader(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, 100)
	conn := NewSharedUDPConn(context.Background(), nil, unhandled)
	defer func() {
		close(unhandled)
		conn.WaitDrained()
	}()

	for i := 0; i < 10; i++ {
		unhandled <- testPacket(byte(i))
	}

	buf := make([]byte, 1500)
	for i := 0; i < 10; i++ {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.EqualValues(t, i, buf[0])
		require.Equal(t, netip.MustParseAddrPort("1.2.3.4:30303"), addr)
	}

	require.Zero(t, conn.Dropped())
}

// TestSharedUDPConn_TruncatesOversizedPacket pins the existing copy semantics.
func TestSharedUDPConn_TruncatesOversizedPacket(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, 1)
	conn := NewSharedUDPConn(context.Background(), nil, unhandled)
	defer func() {
		close(unhandled)
		conn.WaitDrained()
	}()

	unhandled <- discover.ReadPacket{
		Data: []byte{1, 2, 3, 4, 5},
		Addr: netip.MustParseAddrPort("1.2.3.4:30303"),
	}

	buf := make([]byte, 2)
	n, _, err := conn.ReadFromUDPAddrPort(buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, []byte{1, 2}, buf)
}

// TestSharedUDPConn_CloseReleasesBlockedReader is what lets the pre-fork
// listener shut down without closing Unhandled out from under its producer.
func TestSharedUDPConn_CloseReleasesBlockedReader(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, 100)
	conn := NewSharedUDPConn(context.Background(), nil, unhandled)

	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 1500)
		_, _, err := conn.ReadFromUDPAddrPort(buf)
		readErr <- err
	}()

	// Let the reader park on the empty buffer.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, conn.Close())

	select {
	case err := <-readErr:
		// Must be a permanent error so go-ethereum's readLoop exits instead of retrying.
		require.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release the blocked reader; listener shutdown would hang")
	}

	require.NoError(t, conn.Close(), "Close must be idempotent")

	close(unhandled)
	conn.WaitDrained()
}

// TestInitDiscV5Listener_CleansUpOnError covers the half-built service: when
// setup fails after the drain goroutine has started, the caller discards the
// service without ever calling Close, so init has to unwind its own resources.
func TestInitDiscV5Listener_CleansUpOnError(t *testing.T) {
	opts := testingDiscoveryOptions(t, testNetConfig.SSV)
	// A malformed bootnode fails ENR parsing in DiscV5Cfg, which is the first
	// error return after the drain goroutine is running.
	opts.DiscV5Opts.Bootnodes = []string{"enr:-not-a-valid-record"}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dvs := &DiscV5Service{
		logger:    testLogger,
		ctx:       ctx,
		cancel:    cancel,
		ssvConfig: testNetConfig.SSV,
		subnets:   opts.DiscV5Opts.Subnets,
	}

	// Reaching the assertions at all already proves drain exited: the cleanup
	// closes Unhandled and waits for it, so a drain that never stopped would
	// hang here instead of returning.
	require.Error(t, dvs.initDiscV5Listener(opts))
	require.Nil(t, dvs.sharedConn, "sharedConn should be released on the error path")
	require.Nil(t, dvs.conn, "udp socket should be released on the error path")

	// The socket is the resource that actually bites, so prove it was freed
	// rather than trusting the nil-ing: rebinding the same port only succeeds
	// if the failed attempt released it.
	good := testingDiscoveryOptions(t, testNetConfig.SSV)
	good.DiscV5Opts.Port = opts.DiscV5Opts.Port
	good.DiscV5Opts.TCPPort = opts.DiscV5Opts.TCPPort

	dvs2 := &DiscV5Service{
		logger:    testLogger,
		ctx:       ctx,
		cancel:    cancel,
		ssvConfig: testNetConfig.SSV,
		subnets:   good.DiscV5Opts.Subnets,
	}
	require.NoError(t, dvs2.initDiscV5Listener(good), "port still held: the failed attempt leaked its socket")
	t.Cleanup(func() {
		dvs2.dv5Listener.Close()
		close(dvs2.sharedConn.Unhandled)
		dvs2.sharedConn.WaitDrained()
	})
}

// TestSharedUDPConn_DrainsWhileClosing guards the shutdown ordering: the
// producer keeps forwarding while the reader shuts down, and must not block or
// panic. Previously Close() closed Unhandled, so an in-flight send panicked
// with "send on closed channel".
func TestSharedUDPConn_DrainsWhileClosing(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, 100)
	conn := NewSharedUDPConn(context.Background(), nil, unhandled)

	var wg sync.WaitGroup
	wg.Add(1)
	var sent atomic.Int64
	go func() {
		defer wg.Done()
		forwardBlocking(unhandled, 5000, &sent)
	}()

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, conn.Close())

	waited := make(chan struct{})
	go func() {
		wg.Wait()
		close(waited)
	}()

	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatalf("producer blocked during shutdown after %d packets", sent.Load())
	}
	require.EqualValues(t, 5000, sent.Load())

	close(unhandled)
	conn.WaitDrained()
}
