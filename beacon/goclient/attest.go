package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
)

func (gc *goClient) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	if provider, isProvider := gc.client.(eth2client.AttestationDataProvider); isProvider {
		gc.waitOneThirdOrValidBlock(uint64(slot))
		attestationData, err := provider.AttestationData(gc.ctx, slot, committeeIndex)
		if err != nil {
			return nil, err
		}
		return attestationData, nil
	}
	return nil, errors.New("client does not support AttestationDataProvider")
}

func (gc *goClient) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, shareKey *bls.SecretKey) (*spec.Attestation, []byte, error) {
	sig, root, err := gc.signAtt(data, shareKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not sign attestation")
	}

	aggregationBitfield := bitfield.NewBitlist(duty.CommitteeLength)
	aggregationBitfield.SetBitAt(duty.ValidatorCommitteeIndex, true)
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)
	return &spec.Attestation{
		AggregationBits: aggregationBitfield,
		Data:            data,
		Signature:       blsSig,
	}, root, nil
}

// SubmitAttestation implements Beacon interface
func (gc *goClient) SubmitAttestation(attestation *spec.Attestation) error {
	if provider, isProvider := gc.client.(eth2client.AttestationsSubmitter); isProvider {
		signingRoot, err := gc.getSigningRoot(attestation.Data)
		if err != nil {
			return errors.Wrap(err, "failed to get signing root")
		}

		if err := gc.slashableAttestationCheck(gc.ctx, signingRoot); err != nil {
			return errors.Wrap(err, "failed attestation slashing protection check")
		}

		return provider.SubmitAttestations(gc.ctx, []*spec.Attestation{attestation})
	}
	return nil
}

// signAtt returns the signature of an attestation data and its signing root.
func (gc *goClient) signAtt(data *spec.AttestationData, shareKey *bls.SecretKey) ([]byte, []byte, error) {
	root, err := gc.getSigningRoot(data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get signing root")
	} else if root.IsEmpty() {
		return nil, nil, errors.New("got an empty signing root")
	}

	return shareKey.SignByte(root[:]).Serialize(), root[:], nil
}
