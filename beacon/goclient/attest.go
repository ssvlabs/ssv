package goclient

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
)

// AttesterDuties returns attester duties for a given epoch.
func (gc *GoClient) AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
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

// GetAttestationData returns attestation data for a given slot (which is same for all 64 committeeIndex
// values that identify Ethereum committees chosen to attest on this slot).
// Note, committeeIndex is an optional parameter that will be used to set AttestationData.Index
// in the resulting data returned from this function.
// Note, result returned is meant to be read-only, it's not safe to modify it (because it will be
// accessed by multiple concurrent readers).
func (gc *GoClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (
	*phase0.AttestationData,
	spec.DataVersion,
	error,
) {
	// Check cache.
	cachedResult := gc.attestationDataCache.Get(slot)
	if cachedResult != nil {
		return withCommitteeIndex(cachedResult.Value(), committeeIndex), spec.DataVersionPhase0, nil
	}

	// Have to make beacon node request and cache the result.
	result, err := func() (*phase0.AttestationData, error) {
		// Requests with the same slot number must lock on the same mutex.
		reqMu := &gc.attestationReqMuPool[uint64(slot)%uint64(len(gc.attestationReqMuPool))]
		reqMu.Lock()
		defer reqMu.Unlock()

		// Prevent making more than 1 beacon node requests in case somebody has already made this
		// request concurrently and succeeded.
		cachedResult := gc.attestationDataCache.Get(slot)
		if cachedResult != nil {
			return cachedResult.Value(), nil
		}

		attDataReqStart := time.Now()
		resp, err := gc.client.AttestationData(gc.ctx, &api.AttestationDataOpts{
			Slot: slot,
		})
		metricsAttesterDataRequest.Observe(time.Since(attDataReqStart).Seconds())
		if err != nil {
			return nil, fmt.Errorf("failed to get attestation data: %w", err)
		}
		if resp == nil {
			return nil, fmt.Errorf("attestation data response is nil")
		}

		gc.attestationDataCache.Set(slot, resp.Data, ttlcache.DefaultTTL)

		return resp.Data, nil
	}()
	if err != nil {
		return nil, DataVersionNil, err
	}

	return withCommitteeIndex(result, committeeIndex), spec.DataVersionPhase0, nil
}

// withCommitteeIndex creates a shallow copy of data setting committeeIndex to the provided one, the
// rest of attestation data stays unchanged.
// Note, we don't want to modify the provided data object here because it might be accessed
// concurrently, hence, we return shallow copy. We don't need to return deep copy because
// we expect it to be used for read operations only.
func withCommitteeIndex(data *phase0.AttestationData, committeeIndex phase0.CommitteeIndex) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            data.Slot,
		Index:           committeeIndex,
		BeaconBlockRoot: data.BeaconBlockRoot,
		Source:          data.Source,
		Target:          data.Target,
	}
}

// SubmitAttestations implements Beacon interface
func (gc *GoClient) SubmitAttestations(attestations []*phase0.Attestation) error {
	return gc.client.SubmitAttestations(gc.ctx, attestations)
}
