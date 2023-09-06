package goclient

import (
	"context"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *goClient) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	return gc.client.SubmitBeaconCommitteeSubscriptions(ctx, subscription)
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *goClient) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	return gc.client.SubmitSyncCommitteeSubscriptions(ctx, subscription)
}
