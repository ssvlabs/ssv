package intercept

import (
	"context"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type Interceptor interface {
	InterceptAttesterDuties(
		ctx context.Context,
		epoch phase0.Epoch,
		indices []phase0.ValidatorIndex,
		duties []*v1.AttesterDuty,
	) ([]*v1.AttesterDuty, error)
	InterceptProposerDuties(
		ctx context.Context,
		epoch phase0.Epoch,
		indices []phase0.ValidatorIndex,
		duties []*v1.ProposerDuty,
	) ([]*v1.ProposerDuty, error)
	InterceptAttestationData(
		ctx context.Context,
		slot phase0.Slot,
		committeeIndex phase0.CommitteeIndex,
		data *phase0.AttestationData,
	) (*phase0.AttestationData, error)
	InterceptBlockProposal(
		ctx context.Context,
		slot phase0.Slot,
		randaoReveal phase0.BLSSignature,
		graffiti []byte,
		block *spec.VersionedBeaconBlock,
	) (*spec.VersionedBeaconBlock, error)
	InterceptSubmitAttestations(
		ctx context.Context,
		attestations []*phase0.Attestation,
	) ([]*phase0.Attestation, error)
	InterceptSubmitBlockProposal(
		ctx context.Context,
		block *spec.VersionedSignedBeaconBlock,
	) (*spec.VersionedSignedBeaconBlock, error)
}
