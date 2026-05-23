package validation

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

var (
	tBodyA = []byte("envelope-content-A")
	tBodyB = []byte("envelope-content-B")
)

func tMsgID(b byte) spectypes.MessageID {
	var id spectypes.MessageID
	id[0] = b
	return id
}

func TestOBFTAdmissions_DropsIdentical(t *testing.T) {
	tr := newConsensusAdmissionTracker()
	id := tMsgID(1)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA))
	// Same body redelivered → reject.
	require.ErrorContains(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA),
		"identical content")
}

func TestOBFTAdmissions_AdmitsDistinct(t *testing.T) {
	tr := newConsensusAdmissionTracker()
	id := tMsgID(1)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA))
	// Distinct body from same op — admit (so the protocol layer's Rule 2/3
	// equivocation paths fire).
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyB))
}

// Bucket cap: distinct admissions past consensusValidationMaxDistinctPerOpSlot
// are rejected — saves the BLS cost on rejected envelopes.
func TestOBFTAdmissions_BucketCap(t *testing.T) {
	tr := newConsensusAdmissionTracker()
	id := tMsgID(1)
	for k := 0; k < consensusValidationMaxDistinctPerOpSlot; k++ {
		body := []byte{byte(k)}
		require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, body))
	}
	require.ErrorContains(t,
		tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFF}),
		"too many distinct messages")

	// Different op at same (msgID, slot, kind): independent bucket.
	require.NoError(t,
		tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(3), 1, []byte{0xFF}))

	// Different msgID (= different validator): independent bucket.
	require.NoError(t,
		tr.Admit(tMsgID(2), phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFF}))

	// Different kind: independent bucket.
	require.NoError(t,
		tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 2, []byte{0xFF}))

	// Different slot: independent bucket.
	require.NoError(t,
		tr.Admit(id, phase0.Slot(2), spectypes.OperatorID(2), 1, []byte{0xFF}))
}

// TTL eviction: entries past maxAge auto-evict on next Admit (per-bucket
// eviction is unconditional, not throttled), so capacity recovers
// immediately as entries age out.
func TestOBFTAdmissions_TTLEvictsPerBucket(t *testing.T) {
	tr := newConsensusAdmissionTracker()
	tr.maxAge = 100 * time.Millisecond
	now := time.Unix(1_700_000_000, 0)
	tr.now = func() time.Time { return now }

	id := tMsgID(1)
	bucket := consensusAdmissionBucket{msgID: id, slot: 1, op: spectypes.OperatorID(2), kind: 1}
	for k := 0; k < consensusValidationMaxDistinctPerOpSlot; k++ {
		body := []byte{byte(k)}
		require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, body))
	}
	// Bucket full → reject.
	require.Error(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFF}))

	// Past TTL: next Admit's per-bucket eviction drops the aged entries
	// before checking the cap, so the new admission succeeds.
	now = now.Add(200 * time.Millisecond)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFE}))

	// Underlying bucket should now contain only the fresh admission.
	tr.mu.Lock()
	require.Len(t, tr.buckets[bucket].entries, 1)
	tr.mu.Unlock()
}

// Per-bucket eviction is NOT throttled: even within maxAge/8 of the last
// global sweep (so the global sweep is throttled and won't evict aged
// entries), an Admit on a bucket reclaims its own capacity by walking
// its own short entry list. Without per-bucket eviction (the previous
// throttled-global-sweep design), a bucket whose entries aged AFTER the
// most recent global sweep would stay capped until the next sweep window
// — wrongly rejecting legitimate distinct content.
func TestOBFTAdmissions_PerBucketEvictionUnthrottled(t *testing.T) {
	tr := newConsensusAdmissionTracker()
	tr.maxAge = 1000 * time.Millisecond // throttle = maxAge/8 = 125ms.
	now := time.Unix(1_700_000_000, 0)
	tr.now = func() time.Time { return now }

	idA := tMsgID(1)
	idB := tMsgID(2) // separate bucket — used as a "global sweep refresher"
	bucketA := func(body []byte) error {
		return tr.Admit(idA, phase0.Slot(1), spectypes.OperatorID(2), 1, body)
	}

	// Fill bucket A at t=0 (first Admit also triggers the initial global
	// sweep, setting lastGlobalSweep ≈ now=0).
	for k := 0; k < consensusValidationMaxDistinctPerOpSlot; k++ {
		require.NoError(t, bucketA([]byte{byte(k)}))
	}
	require.Error(t, bucketA([]byte{0xFF}))

	// Refresh lastGlobalSweep to t=900ms via a different bucket. Bucket A's
	// entries are still fresh at this point (cutoff = -100ms < 0 = ts).
	now = now.Add(900 * time.Millisecond)
	require.NoError(t, tr.Admit(idB, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0}))

	// Advance to t=1010ms. Bucket A's entries (ts=0) are now past maxAge=1000ms
	// (cutoff = 10ms > ts=0 → aged). lastGlobalSweep=900ms; the throttle
	// window allows global sweep again only after 900+125=1025ms, so at
	// 1010ms the global sweep is THROTTLED and aged entries on other
	// buckets would persist. The per-bucket eviction inside Admit must
	// reclaim bucket A's own capacity.
	now = now.Add(110 * time.Millisecond)
	require.NoError(t, bucketA([]byte{0xFE}),
		"per-bucket eviction must reclaim capacity even when global sweep is throttled")

	// Sanity: bucket A should now contain only the fresh entry.
	tr.mu.Lock()
	require.Len(t, tr.buckets[consensusAdmissionBucket{
		msgID: idA, slot: 1, op: spectypes.OperatorID(2), kind: 1,
	}].entries, 1)
	tr.mu.Unlock()
}
