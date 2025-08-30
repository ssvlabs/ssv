package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SyncCommitteeDuties returns sync committee duties for a given epoch
func (gc *GoClient) SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	reqStart := time.Now()
	resp, err := gc.multiClient.SyncCommitteeDuties(ctx, &api.SyncCommitteeDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordMultiClientRequest(ctx, gc.log, "SyncCommitteeDuties", http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch sync committee duties: %w", err), "SyncCommitteeDuties")
	}
	if resp == nil {
		return nil, errMultiClient(fmt.Errorf("sync committee duties response is nil"), "SyncCommitteeDuties")
	}
	if resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("sync committee duties response data is nil"), "SyncCommitteeDuties")
	}

	return resp.Data, nil
}

// GetSyncMessageBlockRoot returns beacon block root for sync committee
func (gc *GoClient) GetSyncMessageBlockRoot(ctx context.Context) (phase0.Root, spec.DataVersion, error) {
	reqStart := time.Now()
	resp, err := gc.multiClient.BeaconBlockRoot(ctx, &api.BeaconBlockRootOpts{
		Block: "head",
	})
	recordMultiClientRequest(ctx, gc.log, "BeaconBlockRoot", http.MethodGet, time.Since(reqStart), err)
	if err != nil {
		return phase0.Root{}, DataVersionNil, errMultiClient(fmt.Errorf("fetch beacon block root: %w", err), "BeaconBlockRoot")
	}
	if resp == nil {
		return phase0.Root{}, DataVersionNil, errMultiClient(fmt.Errorf("beacon block root response is nil"), "BeaconBlockRoot")
	}
	if resp.Data == nil {
		return phase0.Root{}, DataVersionNil, errMultiClient(fmt.Errorf("beacon block root response data is nil"), "BeaconBlockRoot")
	}

	return *resp.Data, spec.DataVersionAltair, nil
}

// SubmitSyncMessages submits a signed sync committee msg
func (gc *GoClient) SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	if gc.withParallelSubmissions {
		return gc.multiClientSubmit(ctx, "SubmitSyncCommitteeMessages", func(ctx context.Context, client Client) error {
			return client.SubmitSyncCommitteeMessages(ctx, msgs)
		})
	}

	reqStart := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeMessages(ctx, msgs)
	recordMultiClientRequest(ctx, gc.log, "SubmitSyncCommitteeMessages", http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit sync committee messages: %w", err), "SubmitSyncCommitteeMessages")
	}

	return nil
}
