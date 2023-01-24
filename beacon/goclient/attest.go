package goclient

import (
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

func (gc *goClient) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	gc.waitOneThirdOrValidBlock(uint64(slot))

	startTime := time.Now()
	attestationData, err := gc.client.AttestationData(gc.ctx, slot, committeeIndex)
	if err != nil {
		return nil, err
	}
	//TODO(oleg) changed from prysmtime
	metricsAttestationDataRequest.WithLabelValues().Observe(time.Since(startTime).Seconds())

	return attestationData, nil
}

// SubmitAttestation implements Beacon interface
func (gc *goClient) SubmitAttestation(attestation *spec.Attestation) error {
	signingRoot, err := gc.getSigningRoot(attestation.Data)
	if err != nil {
		return errors.Wrap(err, "failed to get signing root")
	}

	if err := gc.slashableAttestationCheck(gc.ctx, signingRoot); err != nil {
		return errors.Wrap(err, "failed attestation slashing protection check")
	}

	return gc.client.SubmitAttestations(gc.ctx, []*spec.Attestation{attestation})
}

// getSigningRoot returns signing root
func (gc *goClient) getSigningRoot(data *spec.AttestationData) ([32]byte, error) {
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
func (gc *goClient) waitOneThirdOrValidBlock(slot uint64) {
	delay := gc.network.DivideSlotBy(3 /* a third of the slot duration */)
	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	//TODO(oleg) changed from prysmtime
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time.NewTimer(wait)
	defer t.Stop()
	for range t.C {
		return
	}
}
