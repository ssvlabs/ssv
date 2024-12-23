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
	"go.uber.org/zap"
)

// AttesterDuties returns attester duties for a given epoch.
func (gc *GoClient) AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	resp, err := gc.multiClient.AttesterDuties(ctx, &api.AttesterDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "AttesterDuties"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain attester duties: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "AttesterDuties"),
		)
		return nil, fmt.Errorf("attester duties response is nil")
	}

	return resp.Data, nil
}

// GetAttestationData returns attestation data for a given slot and committeeIndex.
// Multiple calls for the same slot are joined into a single request, after which
// the result is cached for a short duration, deep copied and returned
// with the provided committeeIndex set.
func (gc *GoClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (
	*phase0.AttestationData,
	spec.DataVersion,
	error,
) {
	// Check cache.
	cachedResult := gc.attestationDataCache.Get(slot)
	if cachedResult != nil {
		data, err := withCommitteeIndex(cachedResult.Value(), committeeIndex)
		if err != nil {
			return nil, DataVersionNil, fmt.Errorf("failed to set committee index: %w", err)
		}
		return data, spec.DataVersionPhase0, nil
	}

	// Have to make beacon node request and cache the result.
	result, err, _ := gc.attestationReqInflight.Do(slot, func() (*phase0.AttestationData, error) {
		attDataReqStart := time.Now()
		resp, err := gc.multiClient.AttestationData(gc.ctx, &api.AttestationDataOpts{
			Slot: slot,
		})
		metricsAttesterDataRequest.Observe(time.Since(attDataReqStart).Seconds())
		if err != nil {
			gc.log.Error(clResponseErrMsg,
				zap.String("api", "AttestationData"),
				zap.Error(err),
			)
			return nil, fmt.Errorf("failed to get attestation data: %w", err)
		}
		if resp == nil {
			gc.log.Error(clNilResponseErrMsg,
				zap.String("api", "AttestationData"),
			)
			return nil, fmt.Errorf("attestation data response is nil")
		}
		if resp.Data == nil {
			gc.log.Error(clNilResponseDataErrMsg,
				zap.String("api", "AttestationData"),
			)
			return nil, fmt.Errorf("attestation data is nil")
		}

		// Caching resulting value here (as part of inflight request) guarantees only 1 request
		// will ever be done for a given slot.
		gc.attestationDataCache.Set(slot, resp.Data, ttlcache.DefaultTTL)

		return resp.Data, nil
	})
	if err != nil {
		return nil, DataVersionNil, err
	}

	data, err := withCommitteeIndex(result, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to set committee index: %w", err)
	}
	return data, spec.DataVersionPhase0, nil
}

// withCommitteeIndex returns a deep copy of the attestation data with the provided committee index set.
func withCommitteeIndex(data *phase0.AttestationData, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, error) {
	// Marshal & unmarshal to make a deep copy. This is safer because it won't break silently if
	// a new field is added to AttestationData.
	ssz, err := data.MarshalSSZ()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attestation data: %w", err)
	}
	var cpy phase0.AttestationData
	if err := cpy.UnmarshalSSZ(ssz); err != nil {
		return nil, fmt.Errorf("failed to unmarshal attestation data: %w", err)
	}
	cpy.Index = committeeIndex
	return &cpy, nil
}

// SubmitAttestations implements Beacon interface
func (gc *GoClient) SubmitAttestations(attestations []*phase0.Attestation) error {
	if err := gc.multiClient.SubmitAttestations(gc.ctx, attestations); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitAttestations"),
			zap.Error(err),
		)
		return err
	}

	return nil
}
