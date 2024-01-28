package goclient

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SyncCommitteeDuties returns sync committee duties for a given epoch
func (gc *goClient) SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	resp, err := gc.client.SyncCommitteeDuties(ctx, &api.SyncCommitteeDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
		Common:  gc.commonOpts(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain sync committee duties: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("sync committee duties response is nil")
	}

	return resp.Data, nil
}

// GetSyncMessageBlockRoot returns beacon block root for sync committee
func (gc *goClient) GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, spec.DataVersion, error) {
	reqStart := time.Now()
	resp, err := gc.client.BeaconBlockRoot(gc.ctx, &api.BeaconBlockRootOpts{
		Block:  "head",
		Common: gc.commonOpts(),
	})
	if err != nil {
		return phase0.Root{}, DataVersionNil, fmt.Errorf("failed to obtain beacon block root: %w", err)
	}
	if resp == nil {
		return phase0.Root{}, DataVersionNil, fmt.Errorf("beacon block root response is nil")
	}
	if resp.Data == nil {
		return phase0.Root{}, DataVersionNil, fmt.Errorf("beacon block root data is nil")
	}
	metricsSyncCommitteeDataRequest.Observe(time.Since(reqStart).Seconds())

	return *resp.Data, spec.DataVersionAltair, nil
}

// SubmitSyncMessage submits a signed sync committee msg
func (gc *goClient) SubmitSyncMessage(msg *altair.SyncCommitteeMessage) error {
	if err := gc.client.SubmitSyncCommitteeMessages(gc.ctx, []*altair.SyncCommitteeMessage{msg}); err != nil {
		return err
	}
	return nil
}
