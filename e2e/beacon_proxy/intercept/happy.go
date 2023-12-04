package intercept

import (
	"context"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// HappyInterceptor doesn't change anything for the nodes and validators.
// TODO: log and/or save state about whether validators preformed well on a happy flow
type HappyInterceptor struct {
	validators map[phase0.ValidatorIndex]*validatorState
}

func NewHappyInterceptor(
	validators []*v1.Validator,
) *HappyInterceptor {
	s := &HappyInterceptor{
		validators: make(map[phase0.ValidatorIndex]*validatorState),
	}
	for _, validator := range validators {
		s.validators[validator.Index] = &validatorState{
			validator: validator,
		}
	}
	return s
}

func (s *HappyInterceptor) InterceptAttesterDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.AttesterDuty,
) ([]*v1.AttesterDuty, error) {
	return duties, nil
}

func (s *HappyInterceptor) InterceptAttestationData(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	data *phase0.AttestationData,
) (*phase0.AttestationData, error) {
	return data, nil
}

func (s *HappyInterceptor) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	return duties, nil
}

func (s *HappyInterceptor) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti []byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	return block, nil
}

func (c *HappyInterceptor) InterceptSubmitAttestations(
	ctx context.Context,
	attestations []*phase0.Attestation,
) ([]*phase0.Attestation, error) {
	return attestations, nil
}

func (c *HappyInterceptor) InterceptSubmitBlockProposal(
	ctx context.Context,
	block *spec.VersionedSignedBeaconBlock,
) (*spec.VersionedSignedBeaconBlock, error) {
	return block, nil
}
