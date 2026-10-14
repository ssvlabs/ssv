// Package attribution records which libp2p peer first delivered each operator's signed
// messages to this node. Over a window, an operator's own node is usually the most frequent
// first deliverer of its messages, which lets an exporter attribute peers (and their node
// versions) to operator IDs.
package attribution

import (
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// DefaultBucketDuration is how long each counting bucket lasts. A Snapshot covers the
	// current and the previous bucket, i.e. between one and two bucket durations of history.
	DefaultBucketDuration = 30 * time.Minute
	// DefaultMaxCandidates caps the number of peers tracked per operator per bucket.
	DefaultMaxCandidates = 8
	// maxReportedCandidates caps the peers returned per operator by Snapshot.
	maxReportedCandidates = 3
)

// Candidate is a peer that first-delivered some of an operator's messages.
type Candidate struct {
	PeerID peer.ID
	Count  uint64
	Share  float64
}

// OperatorEvidence summarizes the first deliveries of one operator's messages over the window.
type OperatorEvidence struct {
	OperatorID spectypes.OperatorID
	Samples    uint64
	Candidates []Candidate
}

// operatorCounts holds one operator's first-delivery counts within a bucket. total counts every
// observation, including those whose peer was later evicted from peers.
type operatorCounts struct {
	total uint64
	peers map[peer.ID]uint64
}

type bucket map[spectypes.OperatorID]*operatorCounts

// Tracker counts (first-delivering peer, signer operator) pairs in two rotating buckets.
// It is safe for concurrent use.
type Tracker struct {
	mu             sync.Mutex
	now            func() time.Time
	bucketDuration time.Duration
	maxCandidates  int

	currentStart time.Time
	current      bucket
	previous     bucket
}

// New returns a Tracker that uses the wall clock and the default limits.
func New() *Tracker {
	return newTracker(time.Now, DefaultBucketDuration, DefaultMaxCandidates)
}

func newTracker(now func() time.Time, bucketDuration time.Duration, maxCandidates int) *Tracker {
	return &Tracker{
		now:            now,
		bucketDuration: bucketDuration,
		maxCandidates:  maxCandidates,
		currentStart:   now(),
		current:        bucket{},
		previous:       bucket{},
	}
}

// WindowDuration is the longest span of history a Snapshot can cover.
func (t *Tracker) WindowDuration() time.Duration {
	return 2 * t.bucketDuration
}

// Observe records that peer p was the first to deliver a message signed by operator op.
func (t *Tracker) Observe(p peer.ID, op spectypes.OperatorID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rotateLocked()

	counts, ok := t.current[op]
	if !ok {
		counts = &operatorCounts{peers: make(map[peer.ID]uint64, 1)}
		t.current[op] = counts
	}
	counts.total++
	if _, tracked := counts.peers[p]; !tracked && len(counts.peers) >= t.maxCandidates {
		evictLowest(counts.peers)
	}
	counts.peers[p]++
}

// Snapshot returns the evidence for the given operators (every operator when ops is empty),
// sorted by operator ID. Each operator lists up to 3 candidates, most frequent first.
func (t *Tracker) Snapshot(ops []spectypes.OperatorID) []OperatorEvidence {
	var want map[spectypes.OperatorID]struct{}
	if len(ops) > 0 {
		want = make(map[spectypes.OperatorID]struct{}, len(ops))
		for _, op := range ops {
			want[op] = struct{}{}
		}
	}

	merged := make(map[spectypes.OperatorID]*operatorCounts)
	t.mu.Lock()
	t.rotateLocked()
	for _, b := range []bucket{t.previous, t.current} {
		for op, counts := range b {
			if want != nil {
				if _, ok := want[op]; !ok {
					continue
				}
			}
			m, ok := merged[op]
			if !ok {
				m = &operatorCounts{peers: make(map[peer.ID]uint64, len(counts.peers))}
				merged[op] = m
			}
			m.total += counts.total
			for p, c := range counts.peers {
				m.peers[p] += c
			}
		}
	}
	t.mu.Unlock()

	out := make([]OperatorEvidence, 0, len(merged))
	for op, counts := range merged {
		out = append(out, evidenceFor(op, counts))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OperatorID < out[j].OperatorID })
	return out
}

// evidenceFor builds an operator's evidence. Samples is the total number of observations in the
// window, so each Share is a fraction of every observation, not only of the surviving peers.
func evidenceFor(op spectypes.OperatorID, counts *operatorCounts) OperatorEvidence {
	ev := OperatorEvidence{OperatorID: op, Samples: counts.total}
	candidates := make([]Candidate, 0, len(counts.peers))
	for p, c := range counts.peers {
		candidates = append(candidates, Candidate{PeerID: p, Count: c})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Count != candidates[j].Count {
			return candidates[i].Count > candidates[j].Count
		}
		return candidates[i].PeerID < candidates[j].PeerID
	})
	if len(candidates) > maxReportedCandidates {
		candidates = candidates[:maxReportedCandidates]
	}
	for i := range candidates {
		candidates[i].Share = float64(candidates[i].Count) / float64(ev.Samples)
	}
	ev.Candidates = candidates
	return ev
}

// rotateLocked advances the buckets once the current one has expired. Callers hold t.mu.
func (t *Tracker) rotateLocked() {
	now := t.now()
	elapsed := now.Sub(t.currentStart)
	if elapsed < t.bucketDuration {
		return
	}
	if elapsed < 2*t.bucketDuration {
		t.previous = t.current
	} else {
		t.previous = bucket{}
	}
	t.current = bucket{}
	t.currentStart = now
}

// evictLowest removes the peer with the lowest count (ties broken by peer ID).
func evictLowest(peers map[peer.ID]uint64) {
	var (
		lowest      peer.ID
		lowestCount uint64
		found       bool
	)
	for p, c := range peers {
		if !found || c < lowestCount || (c == lowestCount && p < lowest) {
			lowest, lowestCount, found = p, c, true
		}
	}
	if found {
		delete(peers, lowest)
	}
}
