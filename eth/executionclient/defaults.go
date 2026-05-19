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
	// knob. It is load-bearing for the voluntary-exit broadcast gate in
	// operator/duties (voluntaryExitExecutionSlotsToPostpone is derived from
	// it): every operator uses it to size the worst-case EL-streaming lag
	// before broadcasting their own partial-sig. If operators ran with
	// different values, fast-EL operators with smaller FollowDistance would
	// broadcast before slower peers had received the EL event, dropping the
	// partial-sig at the receiver's dutyCount validation check and reopening
	// the race that PR #2851 was meant to close.
	FollowDistance = 8

	DefaultHealthInvalidationInterval = 10 * time.Second

	DefaultSyncDistanceTolerance = 5

	DefaultBloomRetryAttempts = 3
	DefaultBloomRetryDelay    = 2 * time.Second
)
