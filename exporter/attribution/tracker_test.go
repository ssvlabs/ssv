package attribution

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	peerA = peer.ID("peer-a")
	peerB = peer.ID("peer-b")
	peerC = peer.ID("peer-c")
	peerD = peer.ID("peer-d")
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestTracker(maxCandidates int) (*Tracker, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
	return newTracker(clock.now, 30*time.Minute, maxCandidates), clock
}

func observeN(tr *Tracker, p peer.ID, op spectypes.OperatorID, n int) {
	for i := 0; i < n; i++ {
		tr.Observe(p, op)
	}
}

func TestSnapshot_DominantPeerFirst(t *testing.T) {
	tr, _ := newTestTracker(DefaultMaxCandidates)
	observeN(tr, peerB, 7, 1)
	observeN(tr, peerA, 7, 9)

	got := tr.Snapshot(nil)

	require.Len(t, got, 1)
	require.Equal(t, spectypes.OperatorID(7), got[0].OperatorID)
	require.Equal(t, uint64(10), got[0].Samples)
	require.Equal(t, []Candidate{
		{PeerID: peerA, Count: 9, Share: 0.9},
		{PeerID: peerB, Count: 1, Share: 0.1},
	}, got[0].Candidates)
}

func TestSnapshot_ReportsAtMostThreeCandidates(t *testing.T) {
	tr, _ := newTestTracker(DefaultMaxCandidates)
	observeN(tr, peerA, 1, 4)
	observeN(tr, peerB, 1, 3)
	observeN(tr, peerC, 1, 2)
	observeN(tr, peerD, 1, 1)

	got := tr.Snapshot(nil)

	require.Len(t, got, 1)
	require.Equal(t, uint64(10), got[0].Samples, "samples count every observation, not only reported candidates")
	require.Len(t, got[0].Candidates, 3)
	require.Equal(t, []peer.ID{peerA, peerB, peerC}, []peer.ID{
		got[0].Candidates[0].PeerID, got[0].Candidates[1].PeerID, got[0].Candidates[2].PeerID,
	})
}

func TestSnapshot_FiltersAndSortsOperators(t *testing.T) {
	tr, _ := newTestTracker(DefaultMaxCandidates)
	tr.Observe(peerA, 9)
	tr.Observe(peerA, 2)
	tr.Observe(peerA, 5)

	all := tr.Snapshot(nil)
	require.Equal(t, []spectypes.OperatorID{2, 5, 9}, []spectypes.OperatorID{
		all[0].OperatorID, all[1].OperatorID, all[2].OperatorID,
	})

	only := tr.Snapshot([]spectypes.OperatorID{5})
	require.Len(t, only, 1)
	require.Equal(t, spectypes.OperatorID(5), only[0].OperatorID)

	none := tr.Snapshot([]spectypes.OperatorID{42})
	require.NotNil(t, none)
	require.Empty(t, none)
}

func TestBuckets_RotateAndExpire(t *testing.T) {
	tr, clock := newTestTracker(DefaultMaxCandidates)
	tr.Observe(peerA, 1) // t=0, bucket 1

	clock.advance(31 * time.Minute)
	tr.Observe(peerB, 1) // rotates: A moves to previous, B in current

	got := tr.Snapshot(nil)
	require.Len(t, got, 1)
	require.Equal(t, uint64(2), got[0].Samples, "previous + current buckets are both reported")

	clock.advance(31 * time.Minute) // rotates again: A dropped, B moves to previous
	got = tr.Snapshot(nil)
	require.Len(t, got, 1)
	require.Equal(t, []Candidate{{PeerID: peerB, Count: 1, Share: 1}}, got[0].Candidates)

	clock.advance(61 * time.Minute) // more than two buckets idle: everything expires
	require.Empty(t, tr.Snapshot(nil))
}

func TestObserve_EvictsLowestWhenFull(t *testing.T) {
	tr, _ := newTestTracker(2)
	observeN(tr, peerA, 1, 5)
	observeN(tr, peerB, 1, 3)
	observeN(tr, peerC, 1, 1) // cap reached: lowest (B) is evicted to make room

	got := tr.Snapshot(nil)

	require.Len(t, got, 1)
	require.Equal(t, uint64(9), got[0].Samples, "evicted counts still count towards samples")
	require.Len(t, got[0].Candidates, 2)
	require.Equal(t, peerA, got[0].Candidates[0].PeerID, "the dominant peer must survive eviction")
	require.Equal(t, uint64(5), got[0].Candidates[0].Count)
	require.InDelta(t, 5.0/9.0, got[0].Candidates[0].Share, 1e-9)
	require.Equal(t, peerC, got[0].Candidates[1].PeerID)
	require.Equal(t, uint64(1), got[0].Candidates[1].Count)
	require.InDelta(t, 1.0/9.0, got[0].Candidates[1].Share, 1e-9)
}

func TestSnapshot_SamplesIncludeEvictedPeers(t *testing.T) {
	tr, _ := newTestTracker(2)
	const dominantCount, distinctPeers = 20, 50
	observeN(tr, peerA, 3, dominantCount)
	for i := 0; i < distinctPeers; i++ {
		tr.Observe(peer.ID(fmt.Sprintf("peer-%d", i)), 3)
	}
	const calls = dominantCount + distinctPeers

	got := tr.Snapshot(nil)

	require.Len(t, got, 1)
	require.Equal(t, uint64(calls), got[0].Samples, "samples must equal the number of Observe calls")
	require.Equal(t, peerA, got[0].Candidates[0].PeerID)
	require.Equal(t, uint64(dominantCount), got[0].Candidates[0].Count)
	require.InDelta(t, float64(dominantCount)/float64(calls), got[0].Candidates[0].Share, 1e-9)
}

func TestTracker_ConcurrentUse(t *testing.T) {
	tr := New()
	peers := []peer.ID{peerA, peerB, peerC}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = tr.Snapshot(nil)
			}
		}
	}()
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				tr.Observe(peers[(g+i)%len(peers)], spectypes.OperatorID(i%5))
			}
		}(g)
	}
	wg.Wait()
	close(stop)

	var total uint64
	for _, ev := range tr.Snapshot(nil) {
		total += ev.Samples
	}
	require.Equal(t, uint64(8000), total)
}

func BenchmarkObserve(b *testing.B) {
	tr := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tr.Observe(peerA, spectypes.OperatorID(i%2000))
	}
}
