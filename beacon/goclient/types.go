package goclient

import "github.com/attestantio/go-eth2-client/spec/phase0"

var (
	SyncCommitteeSize                    uint64       = 512
	SyncCommitteeSubnetCount             uint64       = 4
	TargetAggregatorsPerSyncSubcommittee uint64       = 16
	EpochsPerSyncCommitteePeriod         uint64       = 256
	TargetAggregatorsPerCommittee        uint64       = 16
	FarFutureEpoch                       phase0.Epoch = 1<<64 - 1
	GenesisForkVersion                                = phase0.Version{0x0, 0x0, 0x10, 0x20}
)
