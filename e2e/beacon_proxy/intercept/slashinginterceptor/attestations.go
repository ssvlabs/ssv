package slashinginterceptor

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

type AttesterSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*phase0.AttestationData) error
}

var AttesterSlashingTests = []AttesterSlashingTest{
	{
		Name:      "SameSource_HigherTarget_DifferentRoot",
		Slashable: false,
		Apply: func(data *phase0.AttestationData) error {
			data.Target.Epoch += startEndEpochsDiff
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "SameSource_SameTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			return nil
		},
	},
	{
		Name:      "SameSource_SameTarget_DifferentRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			_, err := rand.Read(data.BeaconBlockRoot[:])
			return err
		},
	},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
	//{
	//	Name:      "HigherSource_SameTarget_SameRoot",
	//	Slashable: true,
	//	Apply: func(data *phase0.AttestationData) error {
	//		data.Source.Epoch += startEndEpochsDiff
	//		return nil
	//	},
	//},
	{
		Name:      "LowerSource_HigherTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch--
			return nil
		},
	},
}

// ATTESTER

func (s *SlashingInterceptor) InterceptAttesterDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.AttesterDuty,
) ([]*v1.AttesterDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, gateway := s.requestContext(ctx)

	if s.blockedEpoch(epoch) {
		return []*v1.AttesterDuty{}, nil
	}

	if len(duties) != len(s.validators) {
		return nil, fmt.Errorf("unexpected duty count")
	}

	// Sort duties for deterministic response.
	sort.Slice(duties, func(i, j int) bool {
		return duties[i].ValidatorIndex < duties[j].ValidatorIndex
	})

	for i, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			return nil, fmt.Errorf("validator not found")
		}

		duty.Slot = s.network.FirstSlotAtEpoch(epoch) + phase0.Slot(i)
		duty.CommitteeIndex = phase0.CommitteeIndex(i)

		s.logger.Debug("validator got attester duty",
			zap.Any("epoch", epoch), zap.Any("slot", duty.Slot), zap.Any("gateway", gateway.Name), zap.Any("committee_index", duty.CommitteeIndex), zap.Any("validator", duty.ValidatorIndex))

		if _, ok = state.firstAttesterDuty[gateway]; !ok {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester duty wasn't requested during the start epoch")
			}
			state.firstAttesterDuty[gateway] = duty

			continue
		}

		if _, ok = state.secondAttesterDuty[gateway]; ok {
			return nil, fmt.Errorf("second attester duties already requested")
		}
		if epoch != s.endEpoch {
			return nil, fmt.Errorf("misbehavior: second attester duty wasn't requested during the end epoch")
		}
		state.secondAttesterDuty[gateway] = duty
	}

	return duties, nil
}

func (s *SlashingInterceptor) InterceptAttestationData(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	data *phase0.AttestationData,
) (*phase0.AttestationData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, gateway := s.requestContext(ctx)

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("attestation data requested for blocked epoch %d", epoch)
	}

	for validatorIndex, state := range s.validators {
		// Skip validators that are not in the requested committee.
		if firstDuty, ok := state.firstAttesterDuty[gateway]; ok && firstDuty.Slot == slot && firstDuty.CommitteeIndex == committeeIndex {
			s.logger.Debug("got first attestation data request", zap.Any("epoch", epoch), zap.Any("gateway", gateway.Name), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester data doesn't belong in the start epoch")
			}

			// Have we created the first attestation data for another gateway already?
			// If so, record the same attestation data for this gateway and return it.
			for gw, existingData := range state.firstAttestationData {
				if gw == gateway {
					s.logger.Warn("first attestation data already requested") //NOTE: aggregator might request attestation data again
					return existingData, nil
				}
				state.firstAttestationData[gateway] = existingData
				return existingData, nil
			}

			// Record the first attestation data on this gateway.
			state.firstAttestationData[gateway] = data
			return data, nil
		}

		if secondDuty, ok := state.secondAttesterDuty[gateway]; ok && secondDuty.Slot == slot && secondDuty.CommitteeIndex == committeeIndex {
			s.logger.Debug("got second attestation data request", zap.Any("epoch", epoch), zap.Any("gateway", gateway.Name), zap.Any("slot", slot), zap.Any("validator", validatorIndex))

			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester data doesn't belong in the end epoch")
			}

			// Have we created the second attestation data for another gateway already?
			// If so, record the same attestation data for this gateway and return it.
			for gw, existingData := range state.secondAttestationData {
				if gw == gateway {
					s.logger.Warn("second attestation data already requested") // NOTE: aggregator might request attestation data again
					return existingData, nil
				}
				state.secondAttestationData[gateway] = existingData
				return existingData, nil
			}

			if _, ok := state.firstAttestationData[gateway]; !ok {
				return nil, fmt.Errorf("unexpected state: received second attestation but first attestation doesn't exist")
			}

			modifiedData := &phase0.AttestationData{
				Slot:            slot,
				Index:           committeeIndex,
				BeaconBlockRoot: state.firstAttestationData[gateway].BeaconBlockRoot,
				Source: &phase0.Checkpoint{
					Epoch: state.firstAttestationData[gateway].Source.Epoch,
					Root:  state.firstAttestationData[gateway].Source.Root,
				},
				Target: &phase0.Checkpoint{
					Epoch: state.firstAttestationData[gateway].Target.Epoch,
					Root:  state.firstAttestationData[gateway].Target.Root,
				},
			}

			// Apply the test on the first attestation data.
			s.logger.Debug("Applying test for validator", zap.String("test", state.attesterTest.Name), zap.Uint64("validator", uint64(state.validator.Index)))
			if err := state.attesterTest.Apply(modifiedData); err != nil {
				return nil, fmt.Errorf("failed to apply attester slashing test: %w", err)
			}

			// Record the second attestation data on this gateway.
			state.secondAttestationData[gateway] = modifiedData
			return modifiedData, nil
		}

	}
	return data, nil
}

func (s *SlashingInterceptor) InterceptSubmitAttestations(
	ctx context.Context,
	attestations []*phase0.Attestation,
) ([]*phase0.Attestation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger, gateway := s.requestContext(ctx)

	for _, attestation := range attestations {
		slot := attestation.Data.Slot
		epoch := s.network.EstimatedEpochAtSlot(slot)
		logger := logger.With(
			zap.Any("epoch", epoch),
			zap.Any("slot", slot),
			zap.Any("source", attestation.Data.Source.Epoch),
			zap.Any("target", attestation.Data.Target.Epoch),
		)
		logger.Debug("submit attestation request")

		if s.blockedEpoch(epoch) {
			return nil, fmt.Errorf("attestation submitted for blocked epoch %d", epoch)
		}

		for validatorIndex, state := range s.validators {
			logger := logger.With(zap.Any("validator", validatorIndex))

			// Skip validators that are not in the requested committee.
			if firstDuty, ok := state.firstAttesterDuty[gateway]; ok && firstDuty.Slot == slot && firstDuty.CommitteeIndex == attestation.Data.Index {
				// Record the submitted attestation.
				if _, ok := state.firstSubmittedAttestation[gateway]; ok {
					return nil, fmt.Errorf("first attestation already submitted")
				}

				logger.Debug("got first attestation submission")

				if epoch != s.startEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the start epoch")
				}
				state.firstSubmittedAttestation[gateway] = attestation

				logger.Debug("submitted first attestation")

				continue
			}

			if secondDuty, ok := state.secondAttesterDuty[gateway]; ok && secondDuty.Slot == slot && secondDuty.CommitteeIndex == attestation.Data.Index {
				logger.Debug("got second attestation submission")

				// Record the second submitted attestation.
				if _, ok := state.secondSubmittedAttestation[gateway]; ok {
					return nil, fmt.Errorf("second attestation already submitted")
				}

				if epoch != s.endEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the end epoch")
				}
				state.secondSubmittedAttestation[gateway] = attestation

				logger.Debug("submitted second attestation")

				if state.attesterTest.Slashable {
					return nil, fmt.Errorf("misbehavior: slashable attestation was submitted during the end epoch")
				}

				continue
			}
		}
	}

	return attestations, nil
}
