package bootnode

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type fakeLister struct{ count int }

func (f fakeLister) AllNodes() []*enode.Node { return make([]*enode.Node, f.count) }

type fakeSocket struct {
	age      time.Duration
	everRead bool
}

func (f fakeSocket) ReadStaleness() (time.Duration, bool) { return f.age, f.everRead }

// Common socket states.
var (
	freshSocket     = fakeSocket{age: time.Second, everRead: true}
	staleSocket     = fakeSocket{age: bootReadStaleGrace + time.Minute, everRead: true}
	neverReadSocket = fakeSocket{everRead: false}
)

func newTestHealth(lister nodeLister, socket socketDrainState, now func() time.Time) *bootNodeHealth {
	h := &bootNodeHealth{
		logger:          zap.NewNop(),
		lister:          lister,
		socket:          socket,
		readStaleGrace:  bootReadStaleGrace,
		emptyTableGrace: bootEmptyTableGrace,
		samplePeriod:    bootHealthSamplePeriod,
		now:             now,
	}
	h.lastNonEmpty.Store(now().UnixNano())
	h.sample()
	return h
}

func TestBootNodeHealth_Check(t *testing.T) {
	base := time.Now()
	fixed := func() time.Time { return base }

	// Populated table + fresh socket → healthy.
	require.NoError(t, newTestHealth(fakeLister{count: 5}, freshSocket, fixed).check())

	// Populated table + stale socket → wedged (discv5 should be revalidating those
	// peers and reading responses, but isn't).
	require.ErrorContains(t,
		newTestHealth(fakeLister{count: 5}, staleSocket, fixed).check(),
		"socket not drained")

	// Empty table + never-read socket, within cold-start grace → healthy: a quiet
	// node with no peers produces no reads, so staleness must not flag it
	// (regression for the "quiet socket looks wedged" false positive).
	require.NoError(t,
		newTestHealth(fakeLister{count: 0}, neverReadSocket, fixed).check(),
		"empty/quiet table must not trip the socket-staleness check")

	// Empty table within cold-start grace → healthy; once it stays empty past the
	// grace with nothing arriving on the socket → unhealthy.
	now := base
	h := newTestHealth(fakeLister{count: 0}, neverReadSocket, func() time.Time { return now })
	require.NoError(t, h.check(), "empty table within cold-start grace is tolerated")
	now = base.Add(bootEmptyTableGrace + time.Minute)
	require.ErrorContains(t, h.check(), "routing table empty")

	// Same, but the socket was read once and then went stale: still unhealthy —
	// a dead read path plus an empty table is the definitional failure.
	now = base
	h = newTestHealth(fakeLister{count: 0}, staleSocket, func() time.Time { return now })
	now = base.Add(bootEmptyTableGrace + time.Minute)
	require.ErrorContains(t, h.check(), "socket undrained")
}

// TestBootNodeHealth_EmptyTableButDrained: empty past grace with a drained
// socket is a config mismatch a restart cannot fix, so /healthz must stay
// healthy — and must log, since that log is the only signal left.
func TestBootNodeHealth_EmptyTableButDrained(t *testing.T) {
	base := time.Now()
	now := base
	h := newTestHealth(fakeLister{count: 0}, freshSocket, func() time.Time { return now })

	core, logs := observer.New(zap.ErrorLevel)
	h.logger = zap.New(core)

	now = base.Add(bootEmptyTableGrace + time.Minute)
	require.NoError(t, h.check(), "empty-but-drained is not restart-fixable, must stay healthy")
	require.Equal(t, 1, logs.FilterMessageSnippet("restart cannot fix").Len(),
		"the error log is the only signal in this state")
}

// TestBootNodeHealth_ClockIsSampleDriven: the empty-table clock follows the
// sampler, not probe arrivals — a long probe-free stretch with a populated
// table must not become an instant past-grace verdict when the table empties
// and the next probe lands (the old probe-driven clock failed exactly here).
func TestBootNodeHealth_ClockIsSampleDriven(t *testing.T) {
	base := time.Now()
	now := base
	h := newTestHealth(fakeLister{count: 3}, neverReadSocket, func() time.Time { return now })

	// Sampler keeps running with no probes at all; last populated sample is late.
	now = base.Add(3 * bootEmptyTableGrace)
	h.sample()

	// Table empties; the first probe arrives a minute later.
	h.lister = fakeLister{count: 0}
	now = now.Add(30 * time.Second)
	h.sample()
	now = now.Add(30 * time.Second)
	require.NoError(t, h.check(), "emptiness is measured from the last populated sample, not the last probe")

	// And once genuinely empty past the grace, it still trips.
	now = now.Add(bootEmptyTableGrace)
	h.sample()
	require.ErrorContains(t, h.check(), "routing table empty")
}

// TestBootNodeHealth_CheckReadsSamplesOnly: check must never consult the table
// directly — a probe observes the sampled state (and so stays cheap on the
// public port).
func TestBootNodeHealth_CheckReadsSamplesOnly(t *testing.T) {
	base := time.Now()
	now := base
	h := newTestHealth(fakeLister{count: 3}, freshSocket, func() time.Time { return now })

	// The table empties, but no sample has run since: the verdict must still
	// reflect the sampled (populated) state.
	h.lister = fakeLister{count: 0}
	now = base.Add(bootEmptyTableGrace + time.Minute)
	require.NoError(t, h.check(), "check must read sampled state, not the live table")
}

// TestBootNodeHealth_Sampler: start's ticker goroutine keeps the sampled state
// current until the context is canceled.
func TestBootNodeHealth_Sampler(t *testing.T) {
	lister := &atomicLister{}
	lister.count.Store(2)
	h := newTestHealth(fakeLister{count: 0}, freshSocket, time.Now)
	h.lister = lister
	h.samplePeriod = 5 * time.Millisecond

	h.start(t.Context())
	require.Eventually(t, h.tablePopulated.Load, 2*time.Second, 5*time.Millisecond)

	lister.count.Store(0)
	require.Eventually(t, func() bool { return !h.tablePopulated.Load() }, 2*time.Second, 5*time.Millisecond)
}

// atomicLister lets the sampler goroutine and the test mutate/read the node
// count without a data race.
type atomicLister struct{ count atomic.Int64 }

func (l *atomicLister) AllNodes() []*enode.Node { return make([]*enode.Node, l.count.Load()) }

func TestBootNodeHealth_Handler(t *testing.T) {
	base := time.Now()
	fixed := func() time.Time { return base }

	rr := httptest.NewRecorder()
	newTestHealth(fakeLister{count: 5}, freshSocket, fixed).
		handler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	// Unhealthy → 503 with a fixed, non-disclosing body (the reason is logged).
	rr = httptest.NewRecorder()
	newTestHealth(fakeLister{count: 5}, staleSocket, fixed).
		handler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.Equal(t, "unhealthy\n", rr.Body.String())

	// The endpoints are read-only views: non-GET/HEAD is rejected before the
	// handler runs, HEAD works (some probes use it).
	h := newTestHealth(fakeLister{count: 5}, freshSocket, fixed)
	rr = httptest.NewRecorder()
	getOnly(h.handler())(rr, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)

	rr = httptest.NewRecorder()
	getOnly(h.handler())(rr, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	require.Equal(t, http.StatusOK, rr.Code)
}
