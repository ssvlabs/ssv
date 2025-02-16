package goclient

import (
	"context"
	"fmt"
	"net/http"
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
	start := time.Now()
	resp, err := gc.multiClient.AttesterDuties(ctx, &api.AttesterDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequestDuration(gc.ctx, "AttesterDuties", gc.multiClient.Address(), http.MethodPost, time.Since(start), err)
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

// GetAttestationData returns attestation data for a given slot.
// Multiple calls for the same slot are joined into a single request, after which
// the result is cached for a short duration, deep copied and returned
func (gc *GoClient) GetAttestationData(slot phase0.Slot) (
	*phase0.AttestationData,
	spec.DataVersion,
	error,
) {
	// Check cache.
	cachedResult := gc.attestationDataCache.Get(slot)
	if cachedResult != nil {
		return cachedResult.Value(), spec.DataVersionPhase0, nil
	}

		attDataReqStart := time.Now()
		resp, err := gc.multiClient.AttestationData(gc.ctx, &api.AttestationDataOpts{
			Slot: slot,
		})

		recordRequestDuration(gc.ctx, "AttestationData", gc.multiClient.Address(), http.MethodGet, time.Since(attDataReqStart), err)

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

	return result, spec.DataVersionPhase0, nil
}

// SubmitAttestations implements Beacon interface
func (gc *GoClient) SubmitAttestations(attestations []*spec.VersionedAttestation) error {
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitAttestations"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitAttestations(gc.ctx, &api.SubmitAttestationsOpts{Attestations: attestations})
	recordRequestDuration(gc.ctx, "SubmitAttestations", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted attestations")
	return nil
}
