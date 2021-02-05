package beacon

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// GetAttestationData implements Beacon interface
func (b *prysmGRPC) GetAttestationData(ctx context.Context, slot, committeeIndex uint64) (*ethpb.AttestationData, error) {
	data, err := b.validatorClient.GetAttestationData(ctx, &ethpb.AttestationDataRequest{
		Slot:           slot,
		CommitteeIndex: committeeIndex,
	})
	if err != nil {
		return nil, errors.Wrap(err, "could not request attestation to sign at slot")
	}

	return data, nil
}

// SignAttestation implements Beacon interface
func (b *prysmGRPC) SignAttestation(ctx context.Context, data *ethpb.AttestationData, validatorIndex uint64, committee []uint64) (*ethpb.Attestation, error) {
	sig, err := b.signAtt(ctx, data)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign attestation")
	}

	var indexInCommittee uint64
	var found bool
	for i, vID := range committee {
		if vID == validatorIndex {
			indexInCommittee = uint64(i)
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("validator ID %d not found in committee of %v", validatorIndex, committee)
	}

	aggregationBitfield := bitfield.NewBitlist(uint64(len(committee)))
	aggregationBitfield.SetBitAt(indexInCommittee, true)

	return &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggregationBitfield,
		Signature:       sig,
	}, nil
}

// SubmitAttestation implements Beacon interface
func (b *prysmGRPC) SubmitAttestation(ctx context.Context, attestation *ethpb.Attestation, validatorIndex uint64) error {
	indexedAtt := &ethpb.IndexedAttestation{
		AttestingIndices: []uint64{validatorIndex},
		Data:             attestation.GetData(),
		Signature:        attestation.GetSignature(),
	}

	signingRoot, err := b.getSigningRoot(ctx, attestation.GetData())
	if err != nil {
		return errors.Wrap(err, "failed to get signing root")
	}

	if err := b.slashableAttestationCheck(ctx, indexedAtt, b.validatorPublicKey, signingRoot); err != nil {
		return errors.Wrap(err, "failed attestation slashing protection check")
	}

	if _, err := b.validatorClient.ProposeAttestation(ctx, attestation); err != nil {
		return errors.Wrap(err, "could not submit attestation to beacon node")
	}

	return nil
}

// signAtt returns the signature of an attestation data and its signing root.
func (b *prysmGRPC) signAtt(ctx context.Context, data *ethpb.AttestationData) ([]byte, error) {
	root, err := b.getSigningRoot(ctx, data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get signing root")
	}

	return b.privateKey.SignByte(root[:]).Serialize(), nil
}

// getSigningRoot returns signing root
func (b *prysmGRPC) getSigningRoot(ctx context.Context, data *ethpb.AttestationData) ([32]byte, error) {
	domain, err := b.domainData(ctx, data.GetSlot(), params.BeaconConfig().DomainBeaconAttester[:])
	if err != nil {
		return [32]byte{}, err
	}

	root, err := helpers.ComputeSigningRoot(data, domain.SignatureDomain)
	if err != nil {
		return [32]byte{}, err
	}

	return root, nil
}
