package goclient

import (
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	SyncCommitteeSize                    uint64 = 512
	SyncCommitteeSubnetCount             uint64 = 4
	TargetAggregatorsPerSyncSubcommittee uint64 = 16
	// FarFutureEpoch is the null representation of an epoch.
	FarFutureEpoch   phase0.Epoch = math.MaxUint64
	IntervalsPerSlot uint64       = 3
)
