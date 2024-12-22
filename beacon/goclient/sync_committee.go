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
	"go.uber.org/zap"
)

// SyncCommitteeDuties returns sync committee duties for a given epoch
func (gc *GoClient) SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	resp, err := gc.client.SyncCommitteeDuties(ctx, &api.SyncCommitteeDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SyncCommitteeDuties"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain sync committee duties: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "SyncCommitteeDuties"),
		)
		return nil, fmt.Errorf("sync committee duties response is nil")
	}

	return resp.Data, nil
}

// GetSyncMessageBlockRoot returns beacon block root for sync committee
func (gc *GoClient) GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, spec.DataVersion, error) {
	reqStart := time.Now()
	resp, err := gc.client.BeaconBlockRoot(gc.ctx, &api.BeaconBlockRootOpts{
		Block: "head",
	})
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "BeaconBlockRoot"),
			zap.Error(err),
		)
		return phase0.Root{}, DataVersionNil, fmt.Errorf("failed to obtain beacon block root: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "BeaconBlockRoot"),
		)

		return phase0.Root{}, DataVersionNil, fmt.Errorf("beacon block root response is nil")
	}
	if resp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "BeaconBlockRoot"),
		)
		return phase0.Root{}, DataVersionNil, fmt.Errorf("beacon block root data is nil")
	}
	metricsSyncCommitteeDataRequest.Observe(time.Since(reqStart).Seconds())

	return *resp.Data, spec.DataVersionAltair, nil
}

// SubmitSyncMessages submits a signed sync committee msg
func (gc *GoClient) SubmitSyncMessages(msgs []*altair.SyncCommitteeMessage) error {
	if err := gc.client.SubmitSyncCommitteeMessages(gc.ctx, msgs); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitSyncCommitteeMessages"),
			zap.Error(err),
		)
		return err
	}

	return nil
}
