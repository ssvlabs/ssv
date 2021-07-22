package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/pkg/errors"
)

// SubscribeToCommitteeSubnet is implementation for subscribing committee to subnet (p2p topic)
func (gc *goClient) SubscribeToCommitteeSubnet(subscription []*api.BeaconCommitteeSubscription) error {
	if provider, isProvider := gc.client.(eth2client.BeaconCommitteeSubscriptionsSubmitter); isProvider {
		return provider.SubmitBeaconCommitteeSubscriptions(gc.ctx, subscription)
	}
	return errors.New("client is not support BeaconCommitteeSubscriptionsSubmitter")
}
