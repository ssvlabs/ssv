package intercept

import (
	"context"
	"sync"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
)

type validatorState struct {
	validator            *v1.Validator
	firstAttesterDuty    *v1.AttesterDuty
	firstAttestationData *phase0.AttestationData
	firstBlock           *spec.VersionedBeaconBlock
}

type epochState struct {
	defined bool
	start   phase0.Epoch
	sleep   phase0.Epoch
	end     phase0.Epoch
}

type SlashingInterceptor struct {
	logger             *zap.Logger
	netCfg             networkconfig.NetworkConfig
	validators         map[phase0.ValidatorIndex]*validatorState
	fakeProposerDuties bool
	mu                 sync.RWMutex
	epochState         epochState
}

func NewSlashingInterceptor(
	logger *zap.Logger,
	netCfg networkconfig.NetworkConfig,
	validators []*v1.Validator,
	fakeDoubleProposerDuties bool,
) *SlashingInterceptor {
	s := &SlashingInterceptor{
		logger:             logger,
		netCfg:             netCfg,
		validators:         make(map[phase0.ValidatorIndex]*validatorState),
		fakeProposerDuties: fakeDoubleProposerDuties,
	}
	for _, validator := range validators {
		s.validators[validator.Index] = &validatorState{
			validator: validator,
		}
	}
	return s
}

func (s *SlashingInterceptor) awaitStart(startCh <-chan struct{}) {
	<-startCh

	s.mu.Lock()
	defer s.mu.Unlock()

	s.epochState.defined = true
	s.epochState.start = s.netCfg.Beacon.EstimatedCurrentEpoch() + 1
}

func (s *SlashingInterceptor) InterceptAttesterDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.AttesterDuty,
) ([]*v1.AttesterDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isBlockedEpoch(epoch) {
		return []*v1.AttesterDuty{}, nil
	}

	for _, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			continue
		}
		if state.firstAttesterDuty == nil {
			state.firstAttesterDuty = duty
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

	epoch := s.netCfg.Beacon.EstimatedEpochAtSlot(slot)
	if s.isBlockedEpoch(epoch) {
		s.logger.Warn("unexpected request",
			fields.Name("InterceptAttestationData"),
			fields.Slot(slot),
			fields.Epoch(epoch))

		return nil, nil
	}

	for _, state := range s.validators {
		// Skip validators that are not in the requested committee.
		if state.firstAttesterDuty == nil || state.firstAttesterDuty.Slot != slot ||
			state.firstAttesterDuty.CommitteeIndex != committeeIndex {
			continue
		}

		// Record the first attestation data.
		if state.firstAttestationData == nil {
			state.firstAttestationData = data
			continue
		}

		// Replace source & target in the attestation data with those from the first one,
		// in order to make it slashable.
		data.Source = state.firstAttestationData.Source
		data.Target = state.firstAttestationData.Target
	}
	return data, nil
}

func (s *SlashingInterceptor) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isBlockedEpoch(epoch) {
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

	epoch := s.netCfg.Beacon.EstimatedEpochAtSlot(slot)
	if s.isBlockedEpoch(epoch) {
		s.logger.Warn("unexpected request",
			fields.Name("InterceptBlockProposal"),
			fields.Slot(slot),
			fields.Epoch(epoch))

		return nil, nil
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

		// Replace the slot in the block with that from the first one,
		// in order to make it slashable.
		block.Capella.Slot = state.firstBlock.Capella.Slot
	}
	return block, nil
}

func (s *SlashingInterceptor) isBlockedEpoch(epoch phase0.Epoch) bool {
	return !s.epochState.defined || epoch < s.epochState.start || epoch == s.epochState.sleep || epoch > s.epochState.end
}
