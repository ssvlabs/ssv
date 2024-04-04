package goclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
)

// AttesterDuties returns attester duties for a given epoch.
func (gc *goClient) AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	resp, err := gc.client.AttesterDuties(ctx, &api.AttesterDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain attester duties: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("attester duties response is nil")
	}

	return resp.Data, nil
}

func (gc *goClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error) {
	gc.attestationDataCacheMu.Lock()
	m, ok := gc.attestationDataPendings[SlotAndCommittee{slot, committeeIndex}]
	if ok {
		gc.attestationDataCacheMu.Unlock()
		m.Lock()
		defer m.Unlock()
		attdata, ok := gc.attestationDataCache[SlotAndCommittee{slot, committeeIndex}]
		if !ok {
			return nil, DataVersionNil, fmt.Errorf("attestation data not found in cache")
		}
		return attdata, spec.DataVersionPhase0, nil
	} else {
		m = &sync.Mutex{}
		gc.attestationDataPendings[SlotAndCommittee{slot, committeeIndex}] = m
	}
	m.Lock()
	defer m.Unlock()
	gc.attestationDataCacheMu.Unlock()

	attDataReqStart := time.Now()
	resp, err := gc.client.AttestationData(gc.ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: committeeIndex,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}
	if resp == nil {
		return nil, DataVersionNil, fmt.Errorf("attestation data response is nil")
	}

	gc.attestationDataCacheMu.Lock()
	gc.attestationDataCache[SlotAndCommittee{slot, committeeIndex}] = resp.Data
	gc.attestationDataCacheMu.Unlock()

	metricsAttesterDataRequest.Observe(time.Since(attDataReqStart).Seconds())

	return resp.Data, spec.DataVersionPhase0, nil
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
