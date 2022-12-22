package goclient

import (
	"fmt"
	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// GetSyncMessageBlockRoot returns beacon block root for sync committee
func (gc *goClient) GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, error) {
	if provider, isProvider := gc.client.(eth2client.BeaconBlockRootProvider); isProvider {
		// Wait a 1/3 into the slot.
		go gc.waitOneThirdOrValidBlock(uint64(slot))
		root, err := provider.BeaconBlockRoot(gc.ctx, fmt.Sprint(slot))
		if err != nil || root == nil {
			return phase0.Root{}, err
		}
		return *root, nil
	}
	return phase0.Root{}, errors.New("client does not support BeaconBlockRootProvider")
}

// SubmitSyncMessage submits a signed sync committee msg
func (gc *goClient) SubmitSyncMessage(msg *altair.SyncCommitteeMessage) error {
	if provider, isProvider := gc.client.(eth2client.SyncCommitteeMessagesSubmitter); isProvider {
		if err := provider.SubmitSyncCommitteeMessages(gc.ctx, []*altair.SyncCommitteeMessage{msg}); err != nil {
			return err
		}
		return nil
	}
	return errors.New("client does not support SyncCommitteeMessagesSubmitter")
}
