package goclient

import (
	"context"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
)

// AttesterDuties returns attester duties for a given epoch.
func (gc *goClient) AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	return gc.client.AttesterDuties(ctx, epoch, validatorIndices)
}

func (gc *goClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error) {

	startTime := time.Now()
	attestationData, err := gc.client.AttestationData(gc.ctx, slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, err
	}

	metricsAttesterDataRequest.Observe(time.Since(startTime).Seconds())

	return attestationData, spec.DataVersionPhase0, nil
}

// SubmitAttestation implements Beacon interface
func (gc *goClient) SubmitAttestation(attestation *phase0.Attestation) error {
	signingRoot, err := gc.getSigningRoot(attestation.Data)
	if err != nil {
		return errors.Wrap(err, "failed to get signing root")
	}

	if err := gc.slashableAttestationCheck(gc.ctx, signingRoot); err != nil {
		return errors.Wrap(err, "failed attestation slashing protection check")
	}

	return gc.client.SubmitAttestations(gc.ctx, []*phase0.Attestation{attestation})
}

// getSigningRoot returns signing root
func (gc *goClient) getSigningRoot(data *phase0.AttestationData) ([32]byte, error) {
	epoch := gc.network.EstimatedEpochAtSlot(data.Slot)
	domain, err := gc.DomainData(epoch, spectypes.DomainAttester)
	if err != nil {
		return [32]byte{}, err
	}
	root, err := gc.ComputeSigningRoot(data, domain)
	if err != nil {
		return [32]byte{}, err
	}
	return root, nil
}
