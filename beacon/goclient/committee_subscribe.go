package goclient

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

// SubscribeToCommitteeSubnet is implementation for subscribing committee to subnet (p2p topic)
func (gc *goClient) SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	return gc.client.SubmitBeaconCommitteeSubscriptions(gc.ctx, subscription)
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *goClient) SubmitSyncCommitteeSubscriptions(subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	return gc.client.SubmitSyncCommitteeSubscriptions(gc.ctx, subscription)
}
