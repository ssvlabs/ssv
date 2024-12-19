package goclient

import (
	"context"
	"net/http"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *GoClient) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	start := time.Now()
	err := gc.client.SubmitBeaconCommitteeSubscriptions(ctx, subscription)

	recordRequestDuration(gc.ctx, "SubmitBeaconCommitteeSubscriptions", gc.client.Address(), http.MethodPost, time.Since(start), err)

	return err
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *GoClient) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	start := time.Now()
	err := gc.client.SubmitSyncCommitteeSubscriptions(ctx, subscription)

	recordRequestDuration(gc.ctx, "SubmitSyncCommitteeSubscriptions", gc.client.Address(), http.MethodPost, time.Since(start), err)

	return err
}
