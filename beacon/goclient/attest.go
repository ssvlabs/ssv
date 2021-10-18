package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/go-bitfield"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
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

func (gc *goClient) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	sig, root, err := gc.signAtt(data, pk)
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
func (gc *goClient) signAtt(data *spec.AttestationData, pk []byte) ([]byte, []byte, error) {
	domain, err := gc.getDomain(data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get domain for signing")
	}
	root, err := gc.computeSigningRoot(data, domain[:])
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get root for signing")
	}
	sig, err := gc.signer.SignBeaconAttestation(specAttDataToPrysmAttData(data), domain, pk)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to sign attestation")
	}

	return sig, root[:], nil
}

// specAttDataToPrysmAttData a simple func converting between data types
func specAttDataToPrysmAttData(data *spec.AttestationData) *eth.AttestationData {
	// TODO - adopt github.com/attestantio/go-eth2-client in eth2-key-manager
	return &eth.AttestationData{
		Slot:            types.Slot(data.Slot),
		CommitteeIndex:  types.CommitteeIndex(data.Index),
		BeaconBlockRoot: data.BeaconBlockRoot[:],
		Source: &eth.Checkpoint{
			Epoch: types.Epoch(data.Source.Epoch),
			Root:  data.Source.Root[:],
		},
		Target: &eth.Checkpoint{
			Epoch: types.Epoch(data.Target.Epoch),
			Root:  data.Target.Root[:],
		},
	}
}
