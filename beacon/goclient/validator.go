package goclient

import (
	"encoding/binary"
	"fmt"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

type validatorRegistration struct {
	*api.VersionedSignedValidatorRegistration

	// SubmittedPreviously signifies whether this validator registration has already been
	// submitted previously.
	SubmittedPreviously bool
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
		SubmittedPreviously:                  false,
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

			registrations := make([]*api.VersionedSignedValidatorRegistration, 0)
			for _, r := range gc.registrationList() {
				validatorPk, err := r.PubKey()
				if err != nil {
					gc.log.Error("Failed to get validator pubkey", zap.Error(err), fields.Slot(currentSlot))
					continue
				}
				if !r.SubmittedPreviously || gc.suitableRegistrationSubmissionSlot(validatorPk, currentSlot) {
					registrations = append(registrations, r.VersionedSignedValidatorRegistration)
				}
			}
			if len(registrations) == 0 {
				continue // no validator registrations to submit this time around
			}

			err := gc.submitRegistrationsBatched(currentSlot, registrations)
			if err != nil {
				gc.log.Error("Failed to submit validator registrations", zap.Error(err), fields.Slot(currentSlot))
				continue
			}

			err = gc.markRegistrationsSubmitted(registrations)
			if err != nil {
				gc.log.Error("Failed to mark validator registrations as submitted", zap.Error(err), fields.Slot(currentSlot))
				continue
			}
		}
	}
}

// suitableRegistrationSubmissionSlot returns true if validator (that corresponds to provided pubkey)
// is eligible for validator registration submission in the provided slot.
func (gc *GoClient) suitableRegistrationSubmissionSlot(validatorPk phase0.BLSPubKey, slot phase0.Slot) bool {
	slotsPerEpoch := gc.network.SlotsPerEpoch()
	validatorSample := binary.LittleEndian.Uint64(validatorPk[:8])
	return validatorSample%slotsPerEpoch == uint64(slot)%slotsPerEpoch
}

func (gc *GoClient) registrationList() []*validatorRegistration {
	gc.registrationMu.Lock()
	defer gc.registrationMu.Unlock()

	result := make([]*validatorRegistration, 0, len(gc.registrations))
	for _, registration := range gc.registrations {
		result = append(result, registration)
	}
	return result
}

func (gc *GoClient) markRegistrationsSubmitted(submitted []*api.VersionedSignedValidatorRegistration) error {
	gc.registrationMu.Lock()
	defer gc.registrationMu.Unlock()

	for _, r := range submitted {
		pk, err := r.PubKey()
		if err != nil {
			return err
		}
		gc.registrations[pk].SubmittedPreviously = true
	}
	return nil
}

func (gc *GoClient) submitRegistrationsBatched(slot phase0.Slot, registrations []*api.VersionedSignedValidatorRegistration) error {
	gc.log.Info("going to submit batch validator registrations",
		fields.Slot(slot),
		fields.Count(len(registrations)))

	for len(registrations) != 0 {
		bs := batchSize
		if bs > len(registrations) {
			bs = len(registrations)
		}

		clientAddress := gc.multiClient.Address()
		logger := gc.log.With(
			zap.String("api", "SubmitValidatorRegistrations"),
			zap.String("client_addr", clientAddress))

		start := time.Now()
		err := gc.multiClient.SubmitValidatorRegistrations(gc.ctx, registrations[0:bs])
		recordRequestDuration(gc.ctx, "SubmitValidatorRegistrations", clientAddress, http.MethodPost, time.Since(start), err)
		if err != nil {
			logger.Error(clResponseErrMsg, zap.Error(err))
			return err
		}

		registrations = registrations[bs:]

		logger.Info("submitted batched validator registrations",
			fields.Slot(slot),
			fields.Count(bs))
	}

	return nil
}
