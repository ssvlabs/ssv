package goclient

import (
	"context"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *GoClient) SubmitBeaconCommitteeSubscriptions(
	ctx context.Context,
	subscription []*eth2apiv1.BeaconCommitteeSubscription,
) error {
	return gc.multiClientSubmit(ctx, "SubmitBeaconCommitteeSubscriptions", func(ctx context.Context, client Client) error {
		return client.SubmitBeaconCommitteeSubscriptions(ctx, subscription)
	})
}

// SubmitSyncCommitteeSubscriptions is an implementation for subscribing sync committee to subnet (p2p topic)
func (gc *GoClient) SubmitSyncCommitteeSubscriptions(
	ctx context.Context,
	subscription []*eth2apiv1.SyncCommitteeSubscription,
) error {
	return gc.multiClientSubmit(ctx, "SubmitSyncCommitteeSubscriptions", func(ctx context.Context, client Client) error {
		return client.SubmitSyncCommitteeSubscriptions(ctx, subscription)
	})
}
