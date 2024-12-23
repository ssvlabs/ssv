package goclient

import (
	"context"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"go.uber.org/zap"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *GoClient) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	if err := gc.multiClient.SubmitBeaconCommitteeSubscriptions(ctx, subscription); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitBeaconCommitteeSubscriptions"),
			zap.Error(err),
		)
		return err
	}

	return nil
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *GoClient) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	if err := gc.multiClient.SubmitSyncCommitteeSubscriptions(ctx, subscription); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitSyncCommitteeSubscriptions"),
			zap.Error(err),
		)
		return err
	}

	return nil
}
