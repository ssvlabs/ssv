package beaconproxy

import (
	"context"
	"sync"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Interceptor interface {
	InterceptAttesterDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex, duties []*v1.AttesterDuty) ([]*v1.AttesterDuty, error)
	InterceptProposerDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex, duties []*v1.ProposerDuty) ([]*v1.ProposerDuty, error)
	InterceptAttestationData(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, data phase0.AttestationData) (phase0.AttestationData, error)
	InterceptBlockProposal(ctx context.Context, slot phase0.Slot, randaoReveal phase0.BLSSignature, graffiti []byte, block *spec.VersionedBeaconBlock) (*spec.VersionedBeaconBlock, error)
}

type validatorState struct {
	lastAttesterDuty    *v1.AttesterDuty
	lastAttestationData *phase0.AttestationData
	lastBlock           *spec.VersionedBeaconBlock
}

type SlashingInterceptor struct {
	validators map[phase0.ValidatorIndex]*validatorState
	slashers   map[phase0.ValidatorIndex]struct{}
	mu         sync.RWMutex
}

func NewSlashingInterceptor(validators, slashingValidators []phase0.ValidatorIndex) *SlashingInterceptor {
	s := &SlashingInterceptor{
		validators: make(map[phase0.ValidatorIndex]*validatorState),
	}
	for _, index := range validators {
		s.validators[index] = &validatorState{}
	}
	for _, index := range slashingValidators {
		s.slashers[index] = struct{}{}
	}
	return s
}

func (s *SlashingInterceptor) InterceptAttesterDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex, duties []*v1.AttesterDuty) ([]*v1.AttesterDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, duty := range duties {
		state, ok := s.validators[duty.ValidatorIndex]
		if !ok {
			continue
		}
		if state.lastAttesterDuty == nil {
			state.lastAttesterDuty = duty
			continue
		}
	}
	return duties, nil
}

func (s *SlashingInterceptor) InterceptAttestationData(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, data phase0.AttestationData) (phase0.AttestationData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, state := range s.validators {
		if state.lastAttesterDuty == nil || state.lastAttesterDuty.Slot != slot || state.lastAttesterDuty.CommitteeIndex != committeeIndex {
			continue
		}
		if state.lastAttestationData == nil {
			state.lastAttestationData = &data
			continue
		}
		data.Source = state.lastAttestationData.Source
		data.Target = state.lastAttestationData.Target
	}
	return data, nil
}

func (s *SlashingInterceptor) InterceptProposerDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex, duties []*v1.ProposerDuty) ([]*v1.ProposerDuty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return []*v1.ProposerDuty{}, nil
}

func (s *SlashingInterceptor) InterceptBlockProposal(ctx context.Context, slot phase0.Slot, randaoReveal phase0.BLSSignature, graffiti []byte, block *spec.VersionedBeaconBlock) (*spec.VersionedBeaconBlock, error) {
	if s.lastBlock == nil {
		s.lastBlock = block
		return block, nil
	}
	block.Capella.Slot = s.lastBlock.Capella.Slot
	return block, nil
}
