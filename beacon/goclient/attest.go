package goclient

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

func (gc *goClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, error) {
	gc.waitOneThirdOrValidBlock(slot)

	startTime := time.Now()
	attestationData, err := gc.client.AttestationData(gc.ctx, slot, committeeIndex)
	if err != nil {
		return nil, err
	}

	metricsAttesterDataRequest.Observe(time.Since(startTime).Seconds())

	return attestationData, nil
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

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitOneThirdOrValidBlock(slot phase0.Slot) {
	delay := gc.network.SlotDurationSec() / 3 /* a third of the slot duration */
	finalTime := gc.slotStartTime(slot).Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
}
