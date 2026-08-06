package discovery

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
// listener's read path made this block at unhandledChanSize packets in flight,
// which halted the post-fork listener and stopped the UDP socket being drained.
func TestSharedUDPConn_ForwardingNeverBlocks(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)

	// Well past unhandled's capacity and the internal buffer combined.
	const total = unhandledChanSize + unhandledBufferSize + 5000

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

// TestSharedUDPConn_DrainsAfterCancel pins the invariant documented on drain:
// ctx is a metric context, not a stop signal. DiscV5Service.Close cancels before
// it joins the producer, so a drain that honoured cancellation would let the
// post-fork listener park in its unescapable forwarding send — deadlocking
// shutdown in UDPv5.Close's wg.Wait, not just stalling discovery. It deliberately
// keeps context.Background(): the whole point is a ctx cancelled before the conn
// is built, which t.Context() (cancelled only at cleanup) cannot express.
func TestSharedUDPConn_DrainsAfterCancel(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := NewSharedUDPConn(ctx, zap.NewNop(), nil, unhandled)

	const total = unhandledChanSize + unhandledBufferSize + 5000
	var sent atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardBlocking(unhandled, total, &sent)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("forwarding blocked after %d/%d packets with a cancelled ctx: "+
			"drain must outlive cancellation or Close deadlocks joining the producer",
			sent.Load(), total)
	}

	close(unhandled)
	conn.WaitDrained()
}

// TestSharedUDPConn_DeliversPacketsToReader covers the normal path: what the
// producer forwards is what the pre-fork listener reads.
func TestSharedUDPConn_DeliversPacketsToReader(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)
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
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)
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
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)

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

// closeRecordingListener wraps a Listener and records whether Close was called,
// so a test can assert that error-path cleanup tore the wrapped listener down.
type closeRecordingListener struct {
	Listener
	closed *atomic.Bool
}

func (l closeRecordingListener) Close() {
	l.closed.Store(true)
	l.Listener.Close()
}

// TestInitDiscV5Listener_CleansUpOnError covers the half-built service: setup
// can fail at several points after resources are live, and the caller discards
// the service without ever calling Close, so init has to unwind its own.
func TestInitDiscV5Listener_CleansUpOnError(t *testing.T) {
	newService := func(ctx context.Context, cancel context.CancelFunc, opts *Options) *DiscV5Service {
		return &DiscV5Service{
			logger:    testLogger,
			ctx:       ctx,
			cancel:    cancel,
			ssvConfig: testNetConfig.SSV,
			subnets:   opts.DiscV5Opts.Subnets,
		}
	}

	cases := []struct {
		name string
		// break the options so init fails at a specific point
		breakSetup func(t *testing.T, o *Options)
		// or break listener creation itself, to fail after resources are live;
		// returns an assertion to run once init has failed and unwound
		breakListener func(t *testing.T) (assertCleanedUp func(t *testing.T))
	}{
		{
			// Fails in DiscV5Cfg, once the drain goroutine is already running.
			name: "malformed bootnode",
			breakSetup: func(t *testing.T, o *Options) {
				o.DiscV5Opts.Bootnodes = []string{"enr:-not-a-valid-record"}
			},
		},
		{
			// Fails in createLocalNode, after the socket is bound but before
			// SharedUDPConn exists — so only the socket needs releasing.
			name: "unusable storage path",
			breakSetup: func(t *testing.T, o *Options) {
				blocker := filepath.Join(t.TempDir(), "not-a-dir")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))
				o.DiscV5Opts.StoragePath = filepath.Join(blocker, "enode")
			},
		},
		{
			// Fails creating the pre-fork listener while the post-fork one is
			// already up, so cleanup must tear that listener (and its socket)
			// down — we wrap it to assert it was actually closed.
			//
			// Swaps the package-level listenV5, so the test must stay non-parallel.
			name: "pre-fork listener creation fails",
			breakListener: func(t *testing.T) func(t *testing.T) {
				var postForkClosed atomic.Bool
				orig := listenV5
				var calls int
				listenV5 = func(conn discover.UDPConn, ln *enode.LocalNode, cfg discover.Config) (Listener, error) {
					// init creates the post-fork listener first, then the
					// pre-fork one; fail the pre-fork and observe the post-fork.
					calls++
					switch calls {
					case 1:
						lis, err := orig(conn, ln, cfg)
						if err != nil {
							return nil, err
						}
						return closeRecordingListener{Listener: lis, closed: &postForkClosed}, nil
					case 2:
						return nil, errors.New("forced pre-fork listener failure")
					default:
						// Later inits (the good rebind below) behave normally.
						return orig(conn, ln, cfg)
					}
				}
				t.Cleanup(func() { listenV5 = orig })

				return func(t *testing.T) {
					require.True(t, postForkClosed.Load(),
						"cleanup must close the post-fork listener when the pre-fork listener fails")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			opts := testingDiscoveryOptions(t, testNetConfig.SSV)
			if tc.breakSetup != nil {
				tc.breakSetup(t, opts)
			}
			var assertCleanedUp func(t *testing.T)
			if tc.breakListener != nil {
				assertCleanedUp = tc.breakListener(t)
			}

			dvs := newService(ctx, cancel, opts)
			// Returning at all already proves drain exited where one was started:
			// the cleanup closes Unhandled and waits on it, so a drain that never
			// stopped would hang here.
			require.Error(t, dvs.initDiscV5Listener(opts))
			require.Nil(t, dvs.sharedConn, "sharedConn should be released on the error path")
			require.Nil(t, dvs.conn, "udp socket should be released on the error path")
			if assertCleanedUp != nil {
				assertCleanedUp(t)
			}

			// The socket is the resource that actually bites, so prove it was
			// freed rather than trusting the nil-ing: rebinding the same port
			// only succeeds if the failed attempt released it.
			good := testingDiscoveryOptions(t, testNetConfig.SSV)
			good.DiscV5Opts.Port = opts.DiscV5Opts.Port
			good.DiscV5Opts.TCPPort = opts.DiscV5Opts.TCPPort

			dvs2 := newService(ctx, cancel, good)
			require.NoError(t, dvs2.initDiscV5Listener(good), "port still held: the failed attempt leaked its socket")
			t.Cleanup(func() {
				require.NoError(t, dvs2.Close())
			})
		})
	}
}

// TestSharedUDPConn_DrainsWhileClosing guards the shutdown ordering: the
// producer keeps forwarding while the reader shuts down, and must not block or
// panic. Previously Close() closed Unhandled, so an in-flight send panicked
// with "send on closed channel".
func TestSharedUDPConn_DrainsWhileClosing(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)

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

// TestSharedUDPConn_DropsOldestWhenFull verifies overflow evicts the oldest
// buffered packet, so the newest are the ones retained rather than tail-dropped.
// Tail-dropping the newcomer would keep the oldest instead and fail this test.
func TestSharedUDPConn_DropsOldestWhenFull(t *testing.T) {
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	conn := NewSharedUDPConn(t.Context(), zap.NewNop(), nil, unhandled)

	// Tag each packet with a unique port so we can tell which survived, and send
	// more than the buffer holds. With no reader, drain fills the buffer and then
	// evicts oldest-first.
	const overflow = 50
	const total = unhandledBufferSize + overflow
	for i := 0; i < total; i++ {
		unhandled <- discover.ReadPacket{
			Data: []byte{0},
			Addr: netip.AddrPortFrom(netip.MustParseAddr("1.2.3.4"), uint16(i)),
		}
	}
	close(unhandled)
	conn.WaitDrained()

	require.EqualValues(t, overflow, conn.Dropped(), "the oldest `overflow` packets should have been dropped")

	// The buffer should hold exactly the newest unhandledBufferSize packets, in
	// order: ports overflow..total-1.
	buf := make([]byte, 1500)
	for want := overflow; want < total; want++ {
		_, addr, err := conn.ReadFromUDPAddrPort(buf)
		require.NoError(t, err)
		require.EqualValues(t, want, addr.Port(), "the newest packets should be retained, in order")
	}
}
