package goclient

import (
	"context"
	"net/http"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"go.uber.org/zap"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *GoClient) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitBeaconCommitteeSubscriptions"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitBeaconCommitteeSubscriptions(ctx, subscription)
	recordRequestDuration(gc.ctx, "SubmitBeaconCommitteeSubscriptions", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted beacon committee subscriptions")
	return nil
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *GoClient) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	start := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeSubscriptions(ctx, subscription)
	recordRequestDuration(gc.ctx, "SubmitSyncCommitteeSubscriptions", gc.multiClient.Address(), http.MethodPost, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitSyncCommitteeSubscriptions"),
			zap.Error(err),
		)
	}

	return err
}
