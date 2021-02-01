package beacon

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	validatorpb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/params"
	ikeymanager "github.com/prysmaticlabs/prysm/validator/keymanager"
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

// SubmitAttestation implements Beacon interface
func (b *prysmGRPC) SubmitAttestation(ctx context.Context, data *ethpb.AttestationData, duty *ethpb.DutiesResponse_Duty, keyManager ikeymanager.IKeymanager) error {
	sig, signingRoot, err := b.signAtt(ctx, b.validatorPublicKey, data, keyManager)
	if err != nil {
		return errors.Wrap(err, "could not sign attestation")
	}

	var indexInCommittee uint64
	var found bool
	for i, vID := range duty.Committee {
		if vID == duty.ValidatorIndex {
			indexInCommittee = uint64(i)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("validator ID %d not found in committee of %v", duty.ValidatorIndex, duty.Committee)
	}

	aggregationBitfield := bitfield.NewBitlist(uint64(len(duty.Committee)))
	aggregationBitfield.SetBitAt(indexInCommittee, true)
	attestation := &ethpb.Attestation{
		Data:            data,
		AggregationBits: aggregationBitfield,
		Signature:       sig,
	}

	indexedAtt := &ethpb.IndexedAttestation{
		AttestingIndices: []uint64{duty.ValidatorIndex},
		Data:             data,

		// Set the signature of the attestation and send it out to the beacon node.
		Signature: sig,
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
func (b *prysmGRPC) signAtt(ctx context.Context, pubKey []byte, data *ethpb.AttestationData, keyManager ikeymanager.IKeymanager) ([]byte, [32]byte, error) {

	domain, root, err := b.getDomainAndSigningRoot(ctx, data)
	if err != nil {
		return nil, [32]byte{}, err
	}

	sig, err := keyManager.Sign(ctx, &validatorpb.SignRequest{
		PublicKey:       pubKey,
		SigningRoot:     root[:],
		SignatureDomain: domain.SignatureDomain,
		Object:          &validatorpb.SignRequest_AttestationData{AttestationData: data},
	})
	if err != nil {
		return nil, [32]byte{}, err
	}

	return sig.Marshal(), root, nil
}

// getDomainAndSigningRoot returns domain data and signing root
func (b *prysmGRPC) getDomainAndSigningRoot(ctx context.Context, data *ethpb.AttestationData) (*ethpb.DomainResponse, [32]byte, error) {
	domain, err := b.domainData(ctx, data.Target.Epoch, params.BeaconConfig().DomainBeaconAttester[:])
	if err != nil {
		return nil, [32]byte{}, err
	}

	root, err := helpers.ComputeSigningRoot(data, domain.SignatureDomain)
	if err != nil {
		return nil, [32]byte{}, err
	}

	return domain, root, nil
}
