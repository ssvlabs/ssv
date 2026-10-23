package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDiscV5Service_DiscoveryStale covers the operator-side accessor: false with
// no socket or before the first read, then driven by the last-read time.
func TestDiscV5Service_DiscoveryStale(t *testing.T) {
	// No socket → never stale.
	require.False(t, (&DiscV5Service{}).DiscoveryStale(3*time.Minute))

	sc := NewTimedConn(nil)
	t0 := sc.LastRead()
	dvs := &DiscV5Service{socketConn: sc}

	// Never read → never stale, however long it sits idle.
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
