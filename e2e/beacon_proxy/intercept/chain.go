package intercept

import (
	"context"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type chain struct {
	interceptors []Interceptor
}

func Chain(interceptors ...Interceptor) Interceptor {
	return &chain{
		interceptors: interceptors,
	}
}
func (c *chain) InterceptAttesterDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.AttesterDuty,
) ([]*v1.AttesterDuty, error) {
	for _, interceptor := range c.interceptors {
		var err error
		duties, err = interceptor.InterceptAttesterDuties(ctx, epoch, indices, duties)
		if err != nil {
			return nil, err
		}
	}
	return duties, nil
}

func (c *chain) InterceptProposerDuties(
	ctx context.Context,
	epoch phase0.Epoch,
	indices []phase0.ValidatorIndex,
	duties []*v1.ProposerDuty,
) ([]*v1.ProposerDuty, error) {
	for _, interceptor := range c.interceptors {
		var err error
		duties, err = interceptor.InterceptProposerDuties(ctx, epoch, indices, duties)
		if err != nil {
			return nil, err
		}
	}
	return duties, nil
}

func (c *chain) InterceptAttestationData(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	data *phase0.AttestationData,
) (*phase0.AttestationData, error) {
	for _, interceptor := range c.interceptors {
		var err error
		data, err = interceptor.InterceptAttestationData(ctx, slot, committeeIndex, data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (c *chain) InterceptBlockProposal(
	ctx context.Context,
	slot phase0.Slot,
	randaoReveal phase0.BLSSignature,
	graffiti []byte,
	block *spec.VersionedBeaconBlock,
) (*spec.VersionedBeaconBlock, error) {
	for _, interceptor := range c.interceptors {
		var err error
		block, err = interceptor.InterceptBlockProposal(ctx, slot, randaoReveal, graffiti, block)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

func (c *chain) InterceptSubmitAttestations(
	ctx context.Context,
	attestations []*phase0.Attestation,
) ([]*phase0.Attestation, error) {
	for _, interceptor := range c.interceptors {
		var err error
		attestations, err = interceptor.InterceptSubmitAttestations(ctx, attestations)
		if err != nil {
			return nil, err
		}
	}
	return attestations, nil
}

func (c *chain) InterceptSubmitBlockProposal(
	ctx context.Context,
	block *spec.VersionedSignedBeaconBlock,
) (*spec.VersionedSignedBeaconBlock, error) {
	for _, interceptor := range c.interceptors {
		var err error
		block, err = interceptor.InterceptSubmitBlockProposal(ctx, block)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}
