package beacon

import (
	"context"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
)

// SubmitAttestation implements Beacon interface
func (b *prysmGRPC) SubmitAttestation(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) error {
	if len(duty.Committee) == 0 {
		return errors.New("empty committee for validator duty, not attesting")
	}

	// TODO: Implement

	/*data, err := b.validatorClient.GetAttestationData(ctx, &ethpb.AttestationDataRequest{
		Slot:           slot,
		CommitteeIndex: duty.CommitteeIndex,
	})
	if err != nil {
		return errors.Wrap(err, "could not request attestation to sign at slot")
	}*/

	return nil
}

/*// signAtt returns the signature of an attestation data and its signing root.
func (b *prysmGRPC) signAtt(ctx context.Context, pubKey [48]byte, data *ethpb.AttestationData) ([]byte, [32]byte, error) {

	domain, root, err := b.getDomainAndSigningRoot(ctx, data)
	if err != nil {
		return nil, [32]byte{}, err
	}

	sig, err := b.keyManager.Sign(ctx, &validatorpb.SignRequest{
		PublicKey:       pubKey[:],
		SigningRoot:     root[:],
		SignatureDomain: domain.SignatureDomain,
		Object:          &validatorpb.SignRequest_AttestationData{AttestationData: data},
	})
	if err != nil {
		return nil, [32]byte{}, err
	}

	return sig.Marshal(), root, nil
}

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
*/
