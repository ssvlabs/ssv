package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
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
	recordRequest(ctx, gc.log, "SyncCommitteeDuties", http.MethodPost, gc.multiClient.Address(), true, time.Since(reqStart), err)
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

// SubmitSyncMessages submits a signed sync committee msg
func (gc *GoClient) SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	if gc.withParallelSubmissions {
		return gc.multiClientSubmit(ctx, "SubmitSyncCommitteeMessages", func(ctx context.Context, client Client) error {
			return client.SubmitSyncCommitteeMessages(ctx, msgs)
		})
	}

	reqStart := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeMessages(ctx, msgs)
	recordRequest(ctx, gc.log, "SubmitSyncCommitteeMessages", http.MethodPost, gc.multiClient.Address(), true, time.Since(reqStart), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit sync committee messages: %w", err), "SubmitSyncCommitteeMessages")
	}

	return nil
}
