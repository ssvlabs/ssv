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
	tr := newOBFTAdmissionTracker()
	id := tMsgID(1)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA))
	// Same body redelivered → reject.
	require.ErrorContains(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA),
		"identical content")
}

func TestOBFTAdmissions_AdmitsDistinct(t *testing.T) {
	tr := newOBFTAdmissionTracker()
	id := tMsgID(1)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyA))
	// Distinct body from same op — admit (so the protocol layer's Rule 2/3
	// equivocation paths fire).
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, tBodyB))
}

// Bucket cap: distinct admissions past obftValidationMaxDistinctPerOpSlot
// are rejected — saves the BLS cost on rejected envelopes.
func TestOBFTAdmissions_BucketCap(t *testing.T) {
	tr := newOBFTAdmissionTracker()
	id := tMsgID(1)
	for k := 0; k < obftValidationMaxDistinctPerOpSlot; k++ {
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

// TTL eviction: entries past maxAge auto-evict on next Admit, decrementing
// counters in lockstep so capacity recovers.
func TestOBFTAdmissions_TTLDecrementsCounter(t *testing.T) {
	tr := newOBFTAdmissionTracker()
	tr.maxAge = 100 * time.Millisecond
	now := time.Unix(1_700_000_000, 0)
	tr.now = func() time.Time { return now }

	id := tMsgID(1)
	for k := 0; k < obftValidationMaxDistinctPerOpSlot; k++ {
		body := []byte{byte(k)}
		require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, body))
	}
	// Bucket full → reject.
	require.Error(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFF}))

	// Past TTL: next Admit triggers sweep, counter decrements, capacity recovers.
	now = now.Add(200 * time.Millisecond)
	require.NoError(t, tr.Admit(id, phase0.Slot(1), spectypes.OperatorID(2), 1, []byte{0xFE}))

	// Underlying maps should now contain only the fresh admission.
	tr.mu.Lock()
	require.Len(t, tr.seen, 1)
	require.Equal(t, 1, tr.counts[obftAdmissionBucket{
		msgID: id, slot: 1, op: spectypes.OperatorID(2), kind: 1,
	}])
	tr.mu.Unlock()
}
