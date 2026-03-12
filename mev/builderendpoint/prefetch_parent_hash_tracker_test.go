package builderendpoint

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func TestPrefetchParentHashTrackerCompare(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0)
	tr := newPrefetchParentHashTracker(2 * time.Second)
	tr.now = func() time.Time { return now }

	slot := phase0.Slot(1)
	pubkey := phase0.BLSPubKey{2}
	parentA := phase0.Hash32{3}
	parentB := phase0.Hash32{4}

	if got := tr.Compare(slot, pubkey, parentA); got != prefetchParentHashCompareMissing {
		t.Fatalf("unexpected compare before record: got %q want %q", got, prefetchParentHashCompareMissing)
	}

	tr.Record(slot, pubkey, parentA)
	if got := tr.Compare(slot, pubkey, parentA); got != prefetchParentHashCompareMatch {
		t.Fatalf("unexpected compare match: got %q want %q", got, prefetchParentHashCompareMatch)
	}
	if got := tr.Compare(slot, pubkey, parentB); got != prefetchParentHashCompareMismatch {
		t.Fatalf("unexpected compare mismatch: got %q want %q", got, prefetchParentHashCompareMismatch)
	}

	now = now.Add(3 * time.Second)
	if got := tr.Compare(slot, pubkey, parentA); got != prefetchParentHashCompareMissing {
		t.Fatalf("unexpected compare after expiry: got %q want %q", got, prefetchParentHashCompareMissing)
	}
}
