package goclient

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *GoClient) GetValidatorData(
	ctx context.Context,
	validatorPubKeys []phase0.BLSPubKey,
) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	reqStart := time.Now()
	resp, err := gc.multiClient.Validators(ctx, &api.ValidatorsOpts{
		State:   "head", // TODO maybe need to get the chainId (head) as var
		PubKeys: validatorPubKeys,
		Common:  api.CommonOpts{Timeout: gc.longTimeout},
	})
	recordMultiClientRequest(ctx, gc.log, "Validators", http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch validators: %w", err), "Validators")
	}
	if resp == nil {
		return nil, errMultiClient(fmt.Errorf("validators response is nil"), "Validators")
	}
	if resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("validators response data is nil"), "Validators")
	}

	return resp.Data, nil
}

// SubmitValidatorRegistrations submits validator registrations, chunking it if necessary.
func (gc *GoClient) SubmitValidatorRegistrations(ctx context.Context, registrations []*api.VersionedSignedValidatorRegistration) error {
	for chunk := range slices.Chunk(registrations, 500) {
		reqStart := time.Now()
		err := gc.multiClient.SubmitValidatorRegistrations(ctx, chunk)
		recordMultiClientRequest(ctx, gc.log, "SubmitValidatorRegistrations", http.MethodPost, time.Since(reqStart), err)
		if err != nil {
			return errMultiClient(fmt.Errorf("submit validator registrations (chunk size = %d): %w", len(chunk), err), "SubmitValidatorRegistrations")
		}
		gc.log.Info("submitted validator registrations", fields.Count(len(chunk)), fields.Duration(reqStart))
	}

	return nil
}
