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

// registrationSubmitter periodically submits validator registrations of 2 types (in batches,
// 1 batch per slot):
// - new validator registrations
// - validator registrations that are relevant for the near future (targeting 10th epoch from now)
// This allows us to keep the amount of registration submissions small and not having to worry
// about pruning gc.registrations "cache" (since it might contain registrations for validators that
// are no longer operating) while still submitting all validator-registrations that matter asap.
func (gc *GoClient) registrationSubmitter(ctx context.Context, slotTickerProvider slotticker.Provider) {
	ticker := slotTickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			config := gc.getBeaconConfig()

			currentSlot := ticker.Slot()
			currentEpoch := config.EstimatedEpochAtSlot(currentSlot)
			slotInEpoch := uint64(currentSlot) % config.SlotsPerEpoch

			// Select registrations to submit.
			targetRegs := make(map[phase0.BLSPubKey]*validatorRegistration, 0)
			gc.registrationMu.Lock()
			// 1. find and add validators participating in the 10th epoch from now
			shares := gc.validatorStore.SelfParticipatingValidators(currentEpoch + 10)
			for _, share := range shares {
				pk := phase0.BLSPubKey{}
				copy(pk[:], share.ValidatorPubKey[:])
				r, ok := gc.registrations[pk]
				if !ok {
					// we haven't constructed the corresponding validator registration for submission yet,
					// so just skip it for now
					continue
				}
				targetRegs[pk] = r
			}
			// 2. find and add newly created validator registrations
			for pk, r := range gc.registrations {
				if r.new {
					targetRegs[pk] = r
				}
			}
			gc.registrationMu.Unlock()

			registrations := make([]*api.VersionedSignedValidatorRegistration, 0)
			for _, r := range targetRegs {
				validatorPk, err := r.PubKey()
				if err != nil {
					gc.log.Error("Failed to get validator pubkey", zap.Error(err), fields.Slot(currentSlot))
					continue
				}

				// Distribute the registrations evenly across the epoch based on the pubkeys.
				validatorDescriptor := xxhash.Sum64(validatorPk[:])
				shouldSubmit := validatorDescriptor%config.SlotsPerEpoch == slotInEpoch

				if r.new || shouldSubmit {
					r.new = false
					registrations = append(registrations, r.VersionedSignedValidatorRegistration)
				}
			}

			// Submit validator registrations in chunks.
			for chunk := range slices.Chunk(registrations, 500) {
				reqStart := time.Now()
				err := gc.multiClient.SubmitValidatorRegistrations(ctx, chunk)
				recordRequestDuration(ctx, "SubmitValidatorRegistrations", gc.multiClient.Address(), http.MethodPost, time.Since(reqStart), err)
				if err != nil {
					gc.log.Error(clResponseErrMsg, zap.Error(err))
					break
				}
				gc.log.Info("submitted validator registrations", fields.Slot(currentSlot), fields.Count(len(chunk)), fields.Duration(reqStart))
			}
		}
	}
}
