package goclient

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
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
	recordRequestDuration(ctx, "Validators", gc.multiClient.Address(), http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Validators"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain validators: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "Validators"),
		)
		return nil, fmt.Errorf("validators response is nil")
	}

	return resp.Data, nil
}

// SubmitValidatorRegistrations submits validator registrations, chunking it if necessary.
func (gc *GoClient) SubmitValidatorRegistrations(ctx context.Context, registrations []*api.VersionedSignedValidatorRegistration) error {
	// Submit validator registrations in chunks.
	for chunk := range slices.Chunk(registrations, 500) {
		// TODO
		for _, registration := range chunk {
			pk, err := registration.PubKey()
			if err != nil {
				panic(fmt.Sprintf("registration.PubKey(): %s", err))
			}
			gc.uniqueRegistrationsMu.Lock()
			gc.uniqueRegistrations[pk] += 1
			gc.uniqueRegistrationsMu.Unlock()
		}

		reqStart := time.Now()
		err := gc.multiClient.SubmitValidatorRegistrations(ctx, chunk)
		recordRequestDuration(ctx, "SubmitValidatorRegistrations", gc.multiClient.Address(), http.MethodPost, time.Since(reqStart), err)
		if err != nil {
			gc.log.Error(clResponseErrMsg, zap.Error(err))
			break
		}
		gc.log.Info("submitted validator registrations", fields.Count(len(chunk)), fields.Duration(reqStart))

		for _, reg := range chunk {
			pk, err := reg.PubKey()
			if err != nil {
				panic(fmt.Sprintf("reg.PubKey(): %s", err))
			}
			gc.log.Info("submitted validator registrations, for validator with pubkey", fields.PubKey(pk[:]))
		}
	}

	gc.uniqueRegistrationsMu.Lock()
	uniqueCnt := len(gc.uniqueRegistrations)
	values := slices.Collect(maps.Values(gc.uniqueRegistrations))
	minSendsCnt := 0
	if len(values) > 0 {
		minSendsCnt = slices.Min(values)
	}
	gc.uniqueRegistrationsMu.Unlock()
	gc.log.Info(fmt.Sprintf("unique validator-registrations sent: %d, minSends: %d", uniqueCnt, minSendsCnt))

	return nil
}
