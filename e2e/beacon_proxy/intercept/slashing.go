package intercept

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"go.uber.org/zap"
)

type ProposerSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*spec.VersionedBeaconBlock) error
}

type AttesterSlashingTest struct {
	Name      string
	Slashable bool
	Apply     func(*phase0.AttestationData) error
}

type validatorState struct {
	validator *v1.Validator

	proposerTest ProposerSlashingTest
	attesterTest AttesterSlashingTest

	// StartEpoch
	firstAttesterDuty         *v1.AttesterDuty
	firstAttestationData      *phase0.AttestationData
	firstSubmittedAttestation *phase0.Attestation

	// EndEpoch
	secondAttesterDuty         *v1.AttesterDuty
	secondAttestationData      *phase0.AttestationData
	secondSubmittedAttestation *phase0.Attestation

	// StartEpoch
	firstProposerDuty   *v1.ProposerDuty
	firstBlock          *spec.VersionedBeaconBlock
	firstSubmittedBlock *spec.VersionedSignedBeaconBlock

	// EndEpoch
	secondProposerDuty   *v1.ProposerDuty
	secondBlock          *spec.VersionedBeaconBlock
	secondSubmittedBlock *spec.VersionedSignedBeaconBlock
}

type SlashingInterceptor struct {
	logger             *zap.Logger
	network            beacon.Network
	startEpoch         phase0.Epoch
	sleepEpoch         phase0.Epoch
	endEpoch           phase0.Epoch
	validators         map[phase0.ValidatorIndex]*validatorState
	fakeProposerDuties bool
	mu                 sync.RWMutex
}

func NewSlashingInterceptor(
	logger *zap.Logger,
	network beacon.Network,
	startEpoch phase0.Epoch,
	fakeProposerDuties bool,
	validators []*v1.Validator,
) *SlashingInterceptor {
	s := &SlashingInterceptor{
		logger:             logger,
		network:            network,
		startEpoch:         startEpoch,
		sleepEpoch:         startEpoch + 1,
		endEpoch:           startEpoch + 2,
		fakeProposerDuties: fakeProposerDuties,
		validators:         make(map[phase0.ValidatorIndex]*validatorState),
	}
	for _, validator := range validators {
		s.validators[validator.Index] = &validatorState{
			validator: validator,
		}
	}
	return s
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

	if s.blockedEpoch(epoch) {
		return []*v1.AttesterDuty{}, nil
	}

	for _, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			continue
		}
		if state.firstAttesterDuty == nil {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester duty wasn't requested during the start epoch")
			}
			state.firstAttesterDuty = duty
			continue
		}
		if state.secondAttesterDuty == nil {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester duty wasn't requested during the end epoch")
			}
			state.secondAttesterDuty = duty
			continue
		}
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

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("attestation data requested for blocked epoch %d", epoch)
	}

	for _, state := range s.validators {
		// TODO: fix to track both first and second attester duties.

		// Skip validators that are not in the requested committee.
		if state.firstAttesterDuty == nil || state.firstAttesterDuty.Slot != slot ||
			state.firstAttesterDuty.CommitteeIndex != committeeIndex {
			continue
		}

		// Record the first attestation data.
		if state.firstAttestationData == nil {
			if epoch != s.startEpoch {
				return nil, fmt.Errorf("misbehavior: first attester data wasn't requested during the start epoch")
			}
			state.firstAttestationData = data
			continue
		}

		// Apply the test on the second attestation data.
		if err := state.attesterTest.Apply(data); err != nil {
			return nil, fmt.Errorf("failed to apply attester slashing test: %w", err)
		}

		// Record the second attestation data.
		if state.secondAttestationData == nil {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: second attester data wasn't requested during the end epoch")
			}
			state.secondAttestationData = data
			continue
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

	for _, attestation := range attestations {
		epoch := s.network.EstimatedEpochAtSlot(attestation.Data.Slot)
		if s.blockedEpoch(epoch) {
			return nil, fmt.Errorf("attestation submitted for blocked epoch %d", epoch)
		}

		for _, state := range s.validators {
			// Skip validators that are not in the attestation's committee.
			if state.firstAttesterDuty == nil || state.firstAttesterDuty.Slot != attestation.Data.Slot ||
				state.firstAttesterDuty.CommitteeIndex != attestation.Data.Index {
				continue
			}

			// Record the submitted attestation.
			if state.firstSubmittedAttestation == nil {
				if epoch != s.endEpoch {
					return nil, fmt.Errorf("misbehavior: attestation wasn't submitted during the end epoch")
				}
				state.firstSubmittedAttestation = attestation
				if state.attesterTest.Slashable {
					return nil, fmt.Errorf("misbehavior: attestation was submitted during the end epoch")
				}
			}
		}
		if s.startEpochExpired(epoch) {
			return nil, fmt.Errorf("misbehavior: start epoch expired")
		}
	}

	return attestations, nil
}

// PROPOSER

func (s *SlashingInterceptor) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blockedEpoch(epoch) {
		return []*v1.ProposerDuty{}, nil
	}

	if !s.fakeProposerDuties {
		return duties, nil
	}

	// Fake 2 proposer duties for each validator.
	duties = make([]*v1.ProposerDuty, 0, len(indices)*2)
	for _, index := range indices {
		state, ok := s.validators[index]
		if !ok {
			continue
		}
		for i := 0; i < 2; i++ {
			duties = append(duties, &v1.ProposerDuty{
				ValidatorIndex: index,
				Slot:           state.firstAttesterDuty.Slot + phase0.Slot(i),
			})
		}
	}
	return duties, nil
}

func (s *SlashingInterceptor) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti []byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal requested for blocked epoch %d", epoch)
	}

	for _, state := range s.validators {
		// Skip forward to the proposer.
		if state.firstAttesterDuty == nil || state.firstAttesterDuty.Slot != slot {
			continue
		}

		// Record the first block.
		if state.firstBlock == nil {
			state.firstBlock = block
			continue
		}

		// Apply the test on the second block.
		if err := state.proposerTest.Apply(block); err != nil {
			return nil, fmt.Errorf("failed to apply proposer slashing test: %w", err)
		}
	}
	return block, nil
}

func (s *SlashingInterceptor) InterceptSubmitBlockProposal(ctx context.Context, block *spec.VersionedSignedBeaconBlock) (*spec.VersionedSignedBeaconBlock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot := block.Capella.Message.Slot
	epoch := s.network.EstimatedEpochAtSlot(slot)
	if s.blockedEpoch(epoch) {
		return nil, fmt.Errorf("block proposal submitted for blocked epoch %d", epoch)
	}

	for _, state := range s.validators {
		if state.firstProposerDuty == nil || state.firstProposerDuty.Slot != slot ||
			state.firstProposerDuty.ValidatorIndex != block.Capella.Message.ProposerIndex {
			continue
		}

		if state.firstSubmittedBlock == nil {
			if epoch != s.endEpoch {
				return nil, fmt.Errorf("misbehavior: proposal wasn't submitted during the end epoch")
			}
			state.firstSubmittedBlock = block
			if state.proposerTest.Slashable {
				return nil, fmt.Errorf("misbehavior: proposal was submitted during the end epoch")
			}
		}
	}

	if s.startEpochExpired(epoch) {
		return nil, fmt.Errorf("misbehavior: start epoch expired")
	}

	return block, nil
}

func (s *SlashingInterceptor) blockedEpoch(epoch phase0.Epoch) bool {
	return epoch < s.startEpoch || epoch == s.sleepEpoch || epoch > s.endEpoch
}

func (s *SlashingInterceptor) startEpochExpired(currentEpoch phase0.Epoch) bool {
	return currentEpoch > s.startEpoch && currentEpoch-s.startEpoch >= 2
}

// TEST CASES

var ProposerSlashingTests = []ProposerSlashingTest{
	{
		Name:      "HigherSlot_DifferentRoot",
		Slashable: false,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot++
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "SameSlot_DifferentRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			_, err := rand.Read(block.Capella.ParentRoot[:])
			return err
		},
	},
	{
		Name:      "LowerSlot_SameRoot",
		Slashable: true,
		Apply: func(block *spec.VersionedBeaconBlock) error {
			switch block.Version {
			case spec.DataVersionCapella:
				block.Capella.Slot--
			default:
				return fmt.Errorf("unsupported version: %s", block.Version)
			}
			return nil
		},
	},
}

var AttesterSlashingTests = []AttesterSlashingTest{
	{
		Name:      "SameSource_HigherTarget_DifferentRoot",
		Slashable: false,
		Apply: func(data *phase0.AttestationData) error {
			data.Target.Epoch++
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
	{
		Name:      "HigherSource_SameTarget_SameRoot",
		Slashable: true,
		Apply: func(data *phase0.AttestationData) error {
			data.Source.Epoch++
			return nil
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
}
