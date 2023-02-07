package duties

import (
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/beacon/goclient"
	"go.uber.org/zap"
)

func (df *dutyFetcher) SyncCommitteeDuties(epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	period := uint64(epoch) / goclient.EpochsPerSyncCommitteePeriod
	firstEpoch := df.firstEpochOfSyncPeriod(period)
	currentEpoch := df.ethNetwork.EstimatedCurrentEpoch()
	if firstEpoch < currentEpoch {
		firstEpoch = currentEpoch
	}
	// If we are in the sync committee that starts at slot x we need to generate a message during slot x-1
	// for it to be included in slot x, hence -1.
	firstSlot := df.ethNetwork.GetEpochFirstSlot(firstEpoch) - 1
	currentSlot := df.ethNetwork.EstimatedCurrentSlot()
	if firstSlot < currentSlot {
		firstSlot = currentSlot
	}
	lastEpoch := df.firstEpochOfSyncPeriod(period+1) - 1
	// If we are in the sync committee that ends at slot x we do not generate a message during slot x-1
	// as it will never be included, hence -1.
	lastSlot := df.ethNetwork.GetEpochFirstSlot(lastEpoch+1) - 2

	syncCommitteeDuties, err := df.beaconClient.SyncCommitteeDuties(firstEpoch, indices)
	if err != nil {
		return nil, err
	}

	if len(syncCommitteeDuties) == 0 {
		return nil, nil
	}

	toSpecDuty := func(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.Duty {
		return &spectypes.Duty{
			Type:                          role,
			PubKey:                        duty.PubKey,
			Slot:                          slot, // in order for the duty ctrl to execute
			ValidatorIndex:                duty.ValidatorIndex,
			ValidatorSyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
		}
	}

	var duties []*spectypes.Duty
	// loop all slots in epoch and add the duties to each slot as sync committee is for each slot
	for slot := firstSlot; slot <= lastSlot; slot++ {
		for _, syncCommitteeDuty := range syncCommitteeDuties {
			duties = append(duties, toSpecDuty(syncCommitteeDuty, slot, spectypes.BNRoleSyncCommittee))
			duties = append(duties, toSpecDuty(syncCommitteeDuty, slot, spectypes.BNRoleSyncCommitteeContribution)) // always trigger contributor as well
		}
	}
	df.logger.Debug("got sync committee duties", zap.String("period", fmt.Sprintf("%d - %d", firstEpoch, lastEpoch)), zap.Int("count", len(duties)))

	syncCommitteeSubscriptions := df.calculateSubscriptions(lastEpoch+1, duties)
	if len(syncCommitteeSubscriptions) > 0 {
		if err := df.beaconClient.SubmitSyncCommitteeSubscriptions(syncCommitteeSubscriptions); err != nil {
			df.logger.Warn("failed to subscribe sync committee to subnet", zap.Error(err))
		}
	}

	return duties, nil
}

// firstEpochOfSyncPeriod calculates the first epoch of the given sync period.
func (df *dutyFetcher) firstEpochOfSyncPeriod(period uint64) phase0.Epoch {
	return phase0.Epoch(period * goclient.EpochsPerSyncCommitteePeriod)
}

// calculateSubscriptions calculates the sync committee subscriptions
// given a set of duties.
func (df *dutyFetcher) calculateSubscriptions(endEpoch phase0.Epoch, duties []*spectypes.Duty) []*eth2apiv1.SyncCommitteeSubscription {
	subscriptions := make([]*eth2apiv1.SyncCommitteeSubscription, 0, len(duties))
	for _, duty := range duties {
		subscriptions = append(subscriptions, &eth2apiv1.SyncCommitteeSubscription{
			ValidatorIndex:       duty.ValidatorIndex,
			SyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			UntilEpoch:           endEpoch,
		})
	}

	return subscriptions
}
