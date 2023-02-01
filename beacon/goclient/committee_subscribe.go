package goclient

import (
	api "github.com/attestantio/go-eth2-client/api/v1"
)

// SubscribeToCommitteeSubnet is implementation for subscribing committee to subnet (p2p topic)
func (gc *goClient) SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error {
	return gc.client.SubmitBeaconCommitteeSubscriptions(gc.ctx, subscription)
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *goClient) SubmitSyncCommitteeSubscriptions(subscription []*api.SyncCommitteeSubscription) error {
	return gc.client.SubmitSyncCommitteeSubscriptions(gc.ctx, subscription)
}
