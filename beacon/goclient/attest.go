package goclient

import (
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	types2 "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/time"
	"github.com/prysmaticlabs/prysm/time/slots"
	time2 "time"
)

func (gc *goClient) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	if provider, isProvider := gc.client.(eth2client.AttestationDataProvider); isProvider {
		gc.waitOneThirdOrValidBlock(uint64(slot))

		startTime := time.Now()
		attestationData, err := provider.AttestationData(gc.ctx, slot, committeeIndex)
		if err != nil {
			return nil, err
		}
		metricsAttestationDataRequest.WithLabelValues().
			Observe(time.Since(startTime).Seconds())

		return attestationData, nil
	}
	return nil, errors.New("client does not support AttestationDataProvider")
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

// getSigningRoot returns signing root
func (gc *goClient) getSigningRoot(data *spec.AttestationData) ([32]byte, error) {
	epoch := gc.network.EstimatedEpochAtSlot(types.Slot(data.Slot))
	domain, err := gc.DomainData(spec.Epoch(epoch), types2.DomainAttester)
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
	delay := slots.DivideSlotBy(3 /* a third of the slot duration */)
	startTime := gc.slotStartTime(slot)
	finalTime := startTime.Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}

	t := time2.NewTimer(wait)
	defer t.Stop()
	for range t.C {
		return
	}
}
