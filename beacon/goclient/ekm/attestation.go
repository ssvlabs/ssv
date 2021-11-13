package ekm

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/go-bitfield"
	eth "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
)

func (km *ethKeyManagerSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	domain, err := km.signingUtils.GetDomain(data)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get domain for signing")
	}
	root, err := km.signingUtils.ComputeSigningRoot(data, domain[:])
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get root for signing")
	}
	sig, err := km.signer.SignBeaconAttestation(specAttDataToPrysmAttData(data), domain, pk)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to sign attestation")
	}
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
	}, root[:], nil
}

func (km *ethKeyManagerSigner) IsAttestationSlashable(data *spec.AttestationData, pk []byte) error {
	status, err := km.slashingProtection.IsSlashableAttestation(pk, specAttDataToPrysmAttData(data))
	if err != nil {
		return errors.Wrap(err, "could not check for slashable attestation")
	}
	if status != nil {
		return errors.Errorf("slashable attestation (%s), not signing", status.Status)
	}
	return nil
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
