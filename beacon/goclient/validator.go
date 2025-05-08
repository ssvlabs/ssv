package goclient

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

type validatorRegistration struct {
	*api.VersionedSignedValidatorRegistration

	// new signifies whether this validator registration has already been submitted previously.
	new bool
}

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *GoClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	reqStart := time.Now()
	resp, err := gc.multiClient.Validators(gc.ctx, &api.ValidatorsOpts{
		State:   "head", // TODO maybe need to get the chainId (head) as var
		PubKeys: validatorPubKeys,
		Common:  api.CommonOpts{Timeout: gc.longTimeout},
	})
	recordRequestDuration(gc.ctx, "Validators", gc.multiClient.Address(), http.MethodPost, time.Since(reqStart), err)
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

// SubmitValidatorRegistration enqueues new validator registration for submission, the submission
// happens asynchronously in a batch with other validator registrations. If validator registration
// already exists it is replaced by this new one.
func (gc *GoClient) SubmitValidatorRegistration(registration *api.VersionedSignedValidatorRegistration) error {
	pk, err := registration.PubKey()
	if err != nil {
		return err
	}

	gc.registrationMu.Lock()
	defer gc.registrationMu.Unlock()

	gc.registrations[pk] = &validatorRegistration{
		VersionedSignedValidatorRegistration: registration,
		new:                                  true,
	}

	return nil
}

// registrationSubmitter periodically submits validator registrations in batches, 1 batch per slot
// making sure
//   - every new(fresh) validator registration is submitted at the earliest slot possible once
//     GoClient is aware of it
//   - every validator registration GoClient is aware of is submitted at least once during 1 epoch
//     period
func (gc *GoClient) registrationSubmitter(slotTickerProvider slotticker.Provider) {
	ticker := slotTickerProvider()
	for {
		select {
		case <-gc.ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot()

			// Select registrations to submit.
			gc.registrationMu.Lock()
			allRegistrations := slices.Collect(maps.Values(gc.registrations))
			gc.registrationMu.Unlock()

			registrations := make([]*api.VersionedSignedValidatorRegistration, 0)
			for _, r := range allRegistrations {
				validatorPk, err := r.PubKey()
				if err != nil {
					gc.log.Error("Failed to get validator pubkey", zap.Error(err), fields.Slot(currentSlot))
					continue
				}

				// Distribute the registrations evenly across the epoch based on the pubkeys.
				slotInEpoch := uint64(currentSlot) % gc.network.SlotsPerEpoch()
				validatorDescriptor := xxhash.Sum64(validatorPk[:])
				shouldSubmit := validatorDescriptor%gc.network.SlotsPerEpoch() == slotInEpoch

				if r.new || shouldSubmit {
					r.new = false
					registrations = append(registrations, r.VersionedSignedValidatorRegistration)
				}
			}

			// Submit validator registrations in chunks.
			// TODO: replace with slices.Chunk after we've upgraded to Go 1.23
			const chunkSize = 500
			for start := 0; start < len(registrations); start += chunkSize {
				end := min(start+chunkSize, len(registrations))
				chunk := registrations[start:end]

				reqStart := time.Now()
				err := gc.multiClient.SubmitValidatorRegistrations(gc.ctx, chunk)
				recordRequestDuration(gc.ctx, "SubmitValidatorRegistrations", gc.multiClient.Address(), http.MethodPost, time.Since(reqStart), err)
				if err != nil {
					gc.log.Error(clResponseErrMsg, zap.Error(err))
					break
				}
				gc.log.Info("submitted validator registrations", fields.Slot(currentSlot), fields.Count(len(chunk)), fields.Duration(reqStart))
			}
		}
	}
}
