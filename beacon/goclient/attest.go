package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
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

func (gc *goClient) SignAttestation(data *spec.AttestationData, duty *spectypes.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return gc.keyManager.SignAttestation(data, duty, pk)
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
