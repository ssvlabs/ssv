package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDiscV5Service_DiscoveryStale covers the operator-side liveness accessor:
// nil before the listener is built, never stale until the socket has actually
// been read, and driven by the wrapped socket's last-read time thereafter.
func TestDiscV5Service_DiscoveryStale(t *testing.T) {
	// No socket wrapped yet (not-yet / failed init) → never stale.
	require.False(t, (&DiscV5Service{}).DiscoveryStale(3*time.Minute))

	sc := NewTimedConn(nil)
	t0 := sc.LastRead()
	dvs := &DiscV5Service{socketConn: sc}

	// Never read → never stale, however long it sits idle: an operator that has
	// simply had no inbound discv5 traffic yet must not be flagged as wedged.
	sc.now = func() time.Time { return t0.Add(time.Hour) }
	require.False(t, dvs.DiscoveryStale(3*time.Minute), "never-read socket is not a wedge")

	// A read arms the signal, stamped at t0.
	sc.read.Store(true)
	sc.lastReadUnixNano.Store(t0.UnixNano())

	sc.now = func() time.Time { return t0.Add(2 * time.Minute) }
	require.False(t, dvs.DiscoveryStale(3*time.Minute), "read within grace → not stale")

	sc.now = func() time.Time { return t0.Add(4 * time.Minute) }
	require.True(t, dvs.DiscoveryStale(3*time.Minute), "read then unread past grace → stale")
}
