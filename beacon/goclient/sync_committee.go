package goclient

import (
	"context"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// SyncCommitteeDuties returns sync committee duties for a given epoch
func (gc *goClient) SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	return gc.client.SyncCommitteeDuties(ctx, epoch, validatorIndices)
}

// GetSyncMessageBlockRoot returns beacon block root for sync committee
func (gc *goClient) GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, spec.DataVersion, error) {
	reqStart := time.Now()
	root, err := gc.client.BeaconBlockRoot(gc.ctx, "head")
	if err != nil {
		return phase0.Root{}, DataVersionNil, err
	}
	if root == nil {
		return phase0.Root{}, DataVersionNil, errors.New("root is nil")
	}

	metricsSyncCommitteeDataRequest.Observe(time.Since(reqStart).Seconds())

	return *root, spec.DataVersionAltair, nil
}

// SubmitSyncMessage submits a signed sync committee msg
func (gc *goClient) SubmitSyncMessage(msg *altair.SyncCommitteeMessage) error {
	if err := gc.client.SubmitSyncCommitteeMessages(gc.ctx, []*altair.SyncCommitteeMessage{msg}); err != nil {
		return err
	}
	return nil
}
