package executionclient

import (
	"time"
)

const (
	DefaultReqTimeout    = 10 * time.Second
	DefaultReqRetryDelay = 10 * time.Second

	// FollowDistance is the offset (in blocks) into the past from the chain head
	// at which a block is considered very likely finalized. The EL log stream
	// only surfaces events from blocks at this depth or deeper, trading some
	// latency for reorg-safety: surfacing an event from a block that the
	// canonical chain may yet rewrite would let operators act on stale
	// validator-share or cluster-membership state, corrupting slashing-
	// protection records (see eth/eventhandler/handlers.go for the EKM bumping
	// logic that this depth protects).
	//
	// This is a network-wide invariant, not an operator-tunable knob —
	// downstream duty-scheduling logic assumes every operator uses the same
	// value. Treat any change as a coordinated network-wide upgrade.
	FollowDistance = 8

	DefaultHealthInvalidationInterval = 10 * time.Second

	DefaultSyncDistanceTolerance = 5

	DefaultBloomRetryAttempts = 3
	DefaultBloomRetryDelay    = 2 * time.Second
)
