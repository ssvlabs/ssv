package discovery

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeUDPConn is a minimal discover.UDPConn so the read path can be exercised
// without a real socket.
type fakeUDPConn struct {
	readErr error
}

func (f *fakeUDPConn) ReadFromUDPAddrPort(b []byte) (int, netip.AddrPort, error) {
	if f.readErr != nil {
		return 0, netip.AddrPort{}, f.readErr
	}
	n := copy(b, []byte{0x1})
	return n, netip.MustParseAddrPort("1.2.3.4:30303"), nil
}

func (f *fakeUDPConn) WriteToUDPAddrPort(b []byte, _ netip.AddrPort) (int, error) {
	return len(b), nil
}

func (f *fakeUDPConn) Close() error        { return nil }
func (f *fakeUDPConn) LocalAddr() net.Addr { return nil }

// TestTimedConn_SeededNotStale: a freshly constructed conn is not stale — it has
// not been read yet, so there is nothing to flag at startup.
func TestTimedConn_SeededNotStale(t *testing.T) {
	c := NewTimedConn(nil)
	require.False(t, c.StaleFor(3*time.Minute))
}

// TestTimedConn_NeverReadNeverStale: a socket that has never delivered a packet
// is not a wedge — it's a node with no inbound discv5 traffic (unreachable
// bootnodes, blocked UDP), which a restart cannot fix — so it must never report
// stale, however much time passes. Once it has been read, staleness arms as
// usual.
func TestTimedConn_NeverReadNeverStale(t *testing.T) {
	c := NewTimedConn(nil)
	t0 := c.LastRead()

	c.now = func() time.Time { return t0.Add(time.Hour) }
	require.False(t, c.StaleFor(3*time.Minute), "never-read socket must not be stale")
	_, ok := c.ReadStaleness()
	require.False(t, ok, "never-read socket has no staleness to report")

	// A successful read arms the signal; from the read time on, it goes stale.
	c.UDPConn = &fakeUDPConn{}
	_, _, err := c.ReadFromUDPAddrPort(make([]byte, 16))
	require.NoError(t, err)
	age, ok := c.ReadStaleness()
	require.True(t, ok, "a read arms the signal")
	require.Zero(t, age, "staleness resets to the read time")

	c.now = func() time.Time { return t0.Add(time.Hour + 4*time.Minute) }
	require.True(t, c.StaleFor(3*time.Minute), "past grace after a read")
}

// TestTimedConn_StaleForTransitions pins the staleness boundary using an
// injected clock, so no real time passes.
func TestTimedConn_StaleForTransitions(t *testing.T) {
	c := NewTimedConn(nil)
	c.read.Store(true) // staleness only arms once the socket has been read
	t0 := c.LastRead()

	c.now = func() time.Time { return t0.Add(2 * time.Minute) }
	require.False(t, c.StaleFor(3*time.Minute), "within grace")

	c.now = func() time.Time { return t0.Add(3 * time.Minute) }
	require.False(t, c.StaleFor(3*time.Minute), "exactly at grace is not yet stale")

	c.now = func() time.Time { return t0.Add(4 * time.Minute) }
	require.True(t, c.StaleFor(3*time.Minute), "past grace")
}

// TestTimedConn_ReadStampsLastRead: a successful read advances the timestamp,
// so an actively drained socket stays fresh.
func TestTimedConn_ReadStampsLastRead(t *testing.T) {
	c := &TimedConn{UDPConn: &fakeUDPConn{}}
	t0 := time.Now()
	c.lastReadUnixNano.Store(t0.UnixNano())
	c.now = func() time.Time { return t0.Add(time.Hour) }

	n, addr, err := c.ReadFromUDPAddrPort(make([]byte, 16))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, netip.MustParseAddrPort("1.2.3.4:30303"), addr)
	require.Equal(t, t0.Add(time.Hour).UnixNano(), c.LastRead().UnixNano())
}

// TestTimedConn_FailedReadDoesNotStamp: a read error must leave the timestamp
// untouched, so a wedged/closed socket cannot look freshly drained.
func TestTimedConn_FailedReadDoesNotStamp(t *testing.T) {
	c := &TimedConn{UDPConn: &fakeUDPConn{readErr: errors.New("boom")}}
	t0 := time.Now()
	c.lastReadUnixNano.Store(t0.UnixNano())
	c.now = func() time.Time { return t0.Add(time.Hour) }

	_, _, err := c.ReadFromUDPAddrPort(make([]byte, 16))
	require.Error(t, err)
	require.Equal(t, t0.UnixNano(), c.LastRead().UnixNano(), "errored read must not stamp")
}

// TestReadStalenessSeconds: the gauge flattening keeps never-read (-1) — the
// state the health check deliberately ignores — distinguishable from a
// just-read socket (0), and reports whole seconds once armed.
func TestReadStalenessSeconds(t *testing.T) {
	c := NewTimedConn(nil)
	t0 := c.LastRead()

	c.now = func() time.Time { return t0.Add(time.Hour) }
	require.Equal(t, int64(-1), readStalenessSeconds(c), "never-read reports the sentinel, not an age")

	c.read.Store(true)
	c.lastReadUnixNano.Store(t0.Add(time.Hour).UnixNano())
	require.Equal(t, int64(0), readStalenessSeconds(c), "just read → 0")

	c.now = func() time.Time { return t0.Add(time.Hour + 42*time.Second) }
	require.Equal(t, int64(42), readStalenessSeconds(c))
}
