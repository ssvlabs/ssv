package duties

import (
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/bloxapp/ssv/beacon/goclient"
	"go.uber.org/zap"
)

func (df *dutyFetcher) SyncCommitteeDuties(epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	period := uint64(epoch) / goclient.EpochsPerSyncCommitteePeriod
	firstEpoch := df.ethNetwork.FirstEpochOfSyncPeriod(period)
	currentEpoch := df.ethNetwork.EstimatedCurrentEpoch()
	if firstEpoch < currentEpoch {
		firstEpoch = currentEpoch
	}
	lastEpoch := df.ethNetwork.FirstEpochOfSyncPeriod(period+1) - 1

	duties, err := df.beaconClient.SyncCommitteeDuties(firstEpoch, indices)
	if err != nil {
		return nil, err
	}

	if len(duties) == 0 {
		return nil, nil
	}

	df.logger.Debug("got sync committee duties", zap.String("period", fmt.Sprintf("%d - %d", firstEpoch, lastEpoch)), zap.Int("count", len(duties)))

	// lastEpoch + 1 due to the fact that we need to subscribe "until" the end of the period
	syncCommitteeSubscriptions := df.calculateSubscriptions(lastEpoch+1, duties)
	if len(syncCommitteeSubscriptions) > 0 {
		if err := df.beaconClient.SubmitSyncCommitteeSubscriptions(syncCommitteeSubscriptions); err != nil {
			df.logger.Warn("failed to subscribe sync committee to subnet", zap.Error(err))
		}
	}

	return duties, nil
}

// calculateSubscriptions calculates the sync committee subscriptions
// given a set of duties.
func (df *dutyFetcher) calculateSubscriptions(endEpoch phase0.Epoch, duties []*eth2apiv1.SyncCommitteeDuty) []*eth2apiv1.SyncCommitteeSubscription {
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
