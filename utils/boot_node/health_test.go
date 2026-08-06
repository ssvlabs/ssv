package bootnode

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/require"
)

type fakeLister struct{ count int }

func (f fakeLister) AllNodes() []*enode.Node { return make([]*enode.Node, f.count) }

type fakeSocket struct{ stale bool }

func (f fakeSocket) StaleFor(time.Duration) bool { return f.stale }

func newTestHealth(lister nodeLister, socket socketDrainState, now func() time.Time) *bootNodeHealth {
	h := &bootNodeHealth{
		lister:          lister,
		socket:          socket,
		readStaleGrace:  bootReadStaleGrace,
		emptyTableGrace: bootEmptyTableGrace,
		now:             now,
	}
	h.lastNonEmpty.Store(now().UnixNano())
	return h
}

func TestBootNodeHealth_Check(t *testing.T) {
	base := time.Now()
	fixed := func() time.Time { return base }

	// Populated table + fresh socket → healthy.
	require.NoError(t, newTestHealth(fakeLister{count: 5}, fakeSocket{stale: false}, fixed).check())

	// Populated table + stale socket → wedged (discv5 should be revalidating those
	// peers and reading responses, but isn't).
	require.ErrorContains(t,
		newTestHealth(fakeLister{count: 5}, fakeSocket{stale: true}, fixed).check(),
		"socket not drained")

	// Empty table + stale socket, within cold-start grace → healthy: a quiet node
	// with no peers produces no reads, so staleness must not flag it (regression
	// for the "quiet socket looks wedged" false positive).
	require.NoError(t,
		newTestHealth(fakeLister{count: 0}, fakeSocket{stale: true}, fixed).check(),
		"empty/quiet table must not trip the socket-staleness check")

	// Empty table but still within cold-start grace → healthy; once it stays
	// empty past the grace → unhealthy.
	now := base
	h := newTestHealth(fakeLister{count: 0}, fakeSocket{stale: false}, func() time.Time { return now })
	require.NoError(t, h.check(), "empty table within cold-start grace is tolerated")
	now = base.Add(bootEmptyTableGrace + time.Minute)
	require.ErrorContains(t, h.check(), "routing table empty")

	// A non-empty observation resets the empty-table clock, so a brief later
	// emptiness is tolerated again.
	now = base
	h = newTestHealth(fakeLister{count: 3}, fakeSocket{stale: false}, func() time.Time { return now })
	now = base.Add(bootEmptyTableGrace + time.Minute) // long after, but table is non-empty here
	require.NoError(t, h.check(), "a non-empty table refreshes the clock")
	h.lister = fakeLister{count: 0} // now empties, but only briefly
	now = now.Add(time.Minute)
	require.NoError(t, h.check(), "just-emptied table is within grace of the last non-empty observation")
}

func TestBootNodeHealth_Handler(t *testing.T) {
	base := time.Now()
	fixed := func() time.Time { return base }

	rr := httptest.NewRecorder()
	newTestHealth(fakeLister{count: 5}, fakeSocket{stale: false}, fixed).
		handler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	rr = httptest.NewRecorder()
	newTestHealth(fakeLister{count: 5}, fakeSocket{stale: true}, fixed).
		handler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}
