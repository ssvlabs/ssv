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
	// latency for reorg-safety (SSV cares strongly: surfacing an event from a
	// later-reorged block could lead to slashing).
	//
	// This value is a network-wide invariant rather than an operator-tunable
	// knob: duty-scheduling logic in operator/duties derives slot offsets from
	// it (see voluntaryExitSlotsToPostpone) so that operators agree on the same
	// scheduled slot deterministically. If operators ran with different values,
	// some would schedule duties in the past on receipt and broadcast pre-
	// consensus messages with diverging slot/epoch values, breaking partial-
	// signature aggregation.
	FollowDistance = 8

	DefaultHealthInvalidationInterval = 10 * time.Second

	DefaultSyncDistanceTolerance = 5

	DefaultBloomRetryAttempts = 3
	DefaultBloomRetryDelay    = 2 * time.Second
)
