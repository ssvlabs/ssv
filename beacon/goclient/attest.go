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
	"github.com/google/uuid"
	"github.com/jellydator/ttlcache/v3"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"
)

type (
	attestationDataResponse struct {
		clientAddr      string
		attestationData *phase0.AttestationData
		score           float64
	}

	attestationDataError struct {
		clientAddr string
		err        error
	}
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
//
// Multiple calls for the same slot are deduplicated and joined into a single inflight request.
// The result is then cached for a short duration and reused for subsequent calls.
//
// If the VOUCH feature (weighted attestation data) is disabled, the function fetches
// attestation data using the simple method only.
//
// When VOUCH is enabled, this method concurrently executes both the weighted and
// simple strategies. The weighted strategy is given a hard timeout window to respond.
// If both complete in time, their results are compared using a scoring mechanism.
// The better result is selected. If only one returns in time, that result is used as a fallback.
//
// Logging is used to track which strategy was selected for each slot, enabling downstream
// observability via tools like Loki without introducing production metrics (counters).
//
// This approach ensures safe rollout of the VOUCH mechanism with performance protections.
//
// [EXPERIMENTAL]: This concurrent fallback mechanism is a hotfix to validate the
// performance of the weighted scoring algorithm in production conditions.
func (gc *GoClient) GetAttestationData(slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
	result, err, _ := gc.attestationReqInflight.Do(slot, func() (*phase0.AttestationData, error) {
		// Check the in-memory cache first to avoid duplicate requests.
		if cached := gc.attestationDataCache.Get(slot); cached != nil {
			return cached.Value(), nil
		}

		// If VOUCH is disabled, run only the simple path and return early.
		if !gc.withWeightedAttestationData {
			data, err := gc.simpleAttestationData(slot)
			if err != nil {
				return nil, err
			}
			gc.attestationDataCache.Set(slot, data, ttlcache.DefaultTTL)
			return data, nil
		}

		// VOUCH is enabled: run both weighted and simple paths concurrently.
		weightedCh := make(chan *phase0.AttestationData, 1)
		simpleCh := make(chan *phase0.AttestationData, 1)
		weightedErrCh := make(chan error, 1)
		simpleErrCh := make(chan error, 1)

		// Apply soft timeout context to the weighted (VOUCH) strategy.
		hardCtx, softCancel := context.WithTimeout(gc.ctx, gc.weightedAttestationDataHardTimeout) // changed to hard timeout (prior was soft) for vouch strategy
		defer softCancel()

		// Launch weightedAttestationData in a goroutine.
		go func() {
			data, err := gc.weightedAttestationData(slot)
			if err != nil {
				weightedErrCh <- err
				return
			}
			weightedCh <- data
		}()

		// Launch simpleAttestationData in a goroutine.
		go func() {
			data, err := gc.simpleAttestationData(slot)
			if err != nil {
				simpleErrCh <- err
				return
			}
			simpleCh <- data
		}()

		var (
			weightedResult *phase0.AttestationData
			simpleResult   *phase0.AttestationData
			weightedErr    error
			simpleErr      error
		)

		// Wait for weighted result or soft timeout.
		weightedTimeout := false // used to indicate if the weighted path hit timeout

		select {
		case weightedResult = <-weightedCh:
			// weighted returned within hard timeout
		case <-hardCtx.Done():
			// hard timeout reached; log (includes time out value) and notify fallback path
			weightedTimeout = true // assign true to flag
			gc.log.With(
				fields.Slot(slot),
				zap.Duration("timeout", gc.weightedAttestationDataHardTimeout),
			).Warn("weighted attestation timed out before completing")
			weightedErrCh <- fmt.Errorf("weighted attestation data timed out (hard timeout)") // sending timed out as error to weighted error channel
		}

		// Wait for the simple path to complete (always required).
		select {
		case simpleResult = <-simpleCh:
		case simpleErr = <-simpleErrCh:
		}

		// Capture weighted error, if it exists and wasn’t already received.
		select {
		case weightedErr = <-weightedErrCh:
		default:
		}

		// If both responses are available, compare and choose the better one.
		if weightedResult != nil && simpleResult != nil {
			// Score both results.
			weightedScore := gc.scoreAttestationData(gc.ctx, weightedResult, gc.log)
			simpleScore := gc.scoreAttestationData(gc.ctx, simpleResult, gc.log)

			if weightedScore > simpleScore {
				// ✨ Added score values to log for observability
				gc.log.With(
					fields.Slot(slot),
					zap.Float64("weighted_score", weightedScore), // New
					zap.Float64("simple_score", simpleScore),     // New
				).Debug("chosen attestation method: weightedAttestationData")
				gc.attestationDataCache.Set(slot, weightedResult, ttlcache.DefaultTTL)
				return weightedResult, nil
			}

			// ✨ Added score values to log for observability
			gc.log.With(
				fields.Slot(slot),
				zap.Float64("weighted_score", weightedScore), // New
				zap.Float64("simple_score", simpleScore),     // New
			).Debug("chosen attestation method: simpleAttestationData")
			gc.attestationDataCache.Set(slot, simpleResult, ttlcache.DefaultTTL)
			return simpleResult, nil
		}

		// If only weighted succeeded
		if weightedResult != nil {
			gc.log.With(fields.Slot(slot)).Debug("fallback to weightedAttestationData")
			gc.attestationDataCache.Set(slot, weightedResult, ttlcache.DefaultTTL)
			return weightedResult, nil
		}

		// If only simple succeeded
		if simpleResult != nil {
			reason := "weighted_error" // default reason
			if weightedTimeout {
				reason = "weighted_timeout" // unless `weightedTimeout` flag is set to true
			}
			gc.log.With(
				fields.Slot(slot),
				zap.String("fallback_reason", reason),
			).Debug("fallback to simpleAttestationData")
			gc.attestationDataCache.Set(slot, simpleResult, ttlcache.DefaultTTL)
			return simpleResult, nil
		}

		// If both methods failed, return appropriate error
		if weightedErr != nil {
			return nil, fmt.Errorf("weighted attestation data failed: %w", weightedErr)
		}
		if simpleErr != nil {
			return nil, fmt.Errorf("simple attestation data failed: %w", simpleErr)
		}

		// Defensive fallback — shouldn't happen
		return nil, fmt.Errorf("both attestation data methods failed")
	})

	if err != nil {
		return nil, DataVersionNil, err
	}

	return result, spec.DataVersionPhase0, nil
}

func (gc *GoClient) weightedAttestationData(slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot), weightedAttestationDataRequestIDField(uuid.New()))
	// We have two timeouts: a soft timeout and a hard timeout.
	// At the soft timeout, we return if we have any responses so far.
	// At the hard timeout, we return unconditionally.
	// The soft timeout is half the duration of the hard timeout.
	ctx, cancel := context.WithTimeout(gc.ctx, gc.weightedAttestationDataHardTimeout)
	defer cancel()

	softCtx, softCancel := context.WithTimeout(ctx, gc.weightedAttestationDataSoftTimeout)
	defer softCancel()

	started := time.Now()

	numberOfRequests := len(gc.clients)
	respCh := make(chan *attestationDataResponse, numberOfRequests)
	errCh := make(chan *attestationDataError, numberOfRequests)

	for _, client := range gc.clients {
		go gc.fetchWeightedAttestationData(ctx, client, respCh, errCh, slot, logger)
	}

	// Wait for all responses (or context done).
	var (
		succeeded,
		errored,
		softTimedOut,
		hardTimedOut int
		bestScore           float64
		bestAttestationData *phase0.AttestationData
		bestClientAddr      string
	)

	for shouldWaitForAttestationDataResponse(succeeded, errored, softTimedOut, numberOfRequests) {
		select {
		case resp := <-respCh:
			succeeded++
			logger.With(
				zap.Duration("elapsed", time.Since(started)),
				zap.String("client_addr", resp.clientAddr),
				zap.Int("succeeded", succeeded),
				zap.Int("errored", errored),
			).Debug("response received")

			if bestAttestationData == nil || resp.score > bestScore {
				bestAttestationData = resp.attestationData
				bestScore = resp.score
				bestClientAddr = resp.clientAddr
			}
		case err := <-errCh:
			errored++
			logger.With(
				zap.Duration("elapsed", time.Since(started)),
				zap.String("client_addr", err.clientAddr),
				zap.Int("succeeded", succeeded),
				zap.Int("errored", errored),
				zap.Error(err.err),
			).Error("error received")
		case <-softCtx.Done():
			softTimedOut = numberOfRequests - (succeeded + errored)

			logger.With(
				zap.Duration("elapsed", time.Since(started)),
				zap.Int("succeeded", succeeded),
				zap.Int("errored", errored),
				zap.Int("soft_timed_out", softTimedOut),
			).Debug("soft timeout reached")
		}
	}

	if succeeded == 0 {
		for shouldWaitForAttestationDataResponse(succeeded, errored, hardTimedOut, numberOfRequests) {
			select {
			case resp := <-respCh:
				succeeded++
				logger.With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", resp.clientAddr),
					zap.Int("succeeded", succeeded),
					zap.Int("errored", errored),
				).Debug("response received")
				if bestAttestationData == nil || resp.score > bestScore {
					bestAttestationData = resp.attestationData
					bestScore = resp.score
					bestClientAddr = resp.clientAddr
				}
			case err := <-errCh:
				errored++
				logger.With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", err.clientAddr),
					zap.Int("succeeded", succeeded),
					zap.Int("errored", errored),
					zap.Error(err.err),
				).Error("received error fetching attestation data")
			case <-ctx.Done():
				hardTimedOut = numberOfRequests - (succeeded + errored)
				logger.With(
					zap.Duration("elapsed", time.Since(started)),
					zap.Int("succeeded", succeeded),
					zap.Int("errored", errored),
					zap.Int("hard_timed_out", hardTimedOut),
				).Error("hard timeout reached")
			}
		}
	}

	resultLogger := logger.With(
		zap.Duration("elapsed", time.Since(started)),
		zap.Int("succeeded", succeeded),
		zap.Int("errored", errored),
		zap.Int("soft_timed_out", softTimedOut),
		zap.Int("hard_timed_out", hardTimedOut),
		zap.Bool("with_weighted_attestation_data", true),
	)
	if bestAttestationData == nil {
		resultLogger.Error("no attestations received")
		return nil, fmt.Errorf("no attestations received")
	}

	resultLogger.With(
		zap.String("client_addr", bestClientAddr),
		zap.Float64("score", bestScore)).
		Debug("successfully fetched attestation data")

	return bestAttestationData, nil
}

func shouldWaitForAttestationDataResponse(responded, errored, timedOut, requestsTotal int) bool {
	return responded+errored+timedOut != requestsTotal
}

func (gc *GoClient) simpleAttestationData(slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot))
	attDataReqStart := time.Now()
	resp, err := gc.multiClient.AttestationData(gc.ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: 0,
	})

	recordRequestDuration(gc.ctx, "AttestationData", gc.multiClient.Address(), http.MethodGet, time.Since(attDataReqStart), err)

	if err != nil {
		logger.Error(clResponseErrMsg,
			zap.String("api", "AttestationData"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get attestation data: %w", err)
	}
	if resp == nil {
		logger.Error(clNilResponseErrMsg, zap.String("api", "AttestationData"))
		return nil, fmt.Errorf("attestation data response is nil")
	}
	if resp.Data == nil {
		logger.Error(clNilResponseDataErrMsg, zap.String("api", "AttestationData"))
		return nil, fmt.Errorf("attestation data is nil")
	}

	logger.With(
		zap.Duration("elapsed", time.Since(attDataReqStart)),
		zap.String("block_root", resp.Data.BeaconBlockRoot.String()), // Add the block_root to the log for simple attestation data method
		zap.Bool("with_weighted_attestation_data", false),
	).Debug("successfully fetched attestation data")

	return resp.Data, nil
}

func (gc *GoClient) fetchWeightedAttestationData(ctx context.Context,
	client Client,
	respCh chan *attestationDataResponse,
	errCh chan *attestationDataError,
	slot phase0.Slot,
	logger *zap.Logger,
) {
	addr := client.Address()
	attDataReqStart := time.Now()

	logger = logger.With(zap.String("client_addr", addr))

	logger.Debug("fetching attestation data")
	response, err := client.AttestationData(ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: 0,
	})

	recordRequestDuration(ctx, "AttestationData", addr, http.MethodGet, time.Since(attDataReqStart), err)

	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		errCh <- &attestationDataError{
			clientAddr: addr,
			err:        err,
		}
		return
	}
	if response == nil {
		logger.Error(clNilResponseErrMsg)
		errCh <- &attestationDataError{
			clientAddr: addr,
			err:        fmt.Errorf("response is nil"),
		}
		return
	}
	attestationData := response.Data
	if attestationData == nil {
		logger.Error(clNilResponseDataErrMsg)
		errCh <- &attestationDataError{
			clientAddr: addr,
			err:        fmt.Errorf("attestation data nil"),
		}
		return
	}

	logger.Debug("scoring attestation data")
	score := gc.scoreAttestationData(ctx, attestationData, logger)

	respCh <- &attestationDataResponse{
		clientAddr:      addr,
		attestationData: attestationData,
		score:           score,
	}
}

// scoreAttestationData generates a score for attestation data.
// The score is relative to the reward expected from the contents of the attestation.
func (gc *GoClient) scoreAttestationData(ctx context.Context,
	attestationData *phase0.AttestationData,
	logger *zap.Logger,
) float64 {
	logger = logger.With(
		fields.BlockRoot(attestationData.BeaconBlockRoot),
		zap.Uint64("attestation_data_slot", uint64(attestationData.Slot)))
	// Initial score is based on height of source and target epochs.
	score := float64(attestationData.Source.Epoch + attestationData.Target.Epoch)

	// Increase score based on the nearness of the head slot.
	slot, err := gc.blockRootToSlot(attestationData.BeaconBlockRoot, logger)
	if err != nil {
		logger.
			With(zap.Error(err)).
			Error("failed to obtain slot for block root")
	} else {
		score += float64(1) / float64(1+attestationData.Slot-slot)
	}

	logger.With(
		zap.Uint64("head_slot", uint64(slot)),
		zap.Uint64("source_epoch", uint64(attestationData.Source.Epoch)),
		zap.Uint64("target_epoch", uint64(attestationData.Target.Epoch)),
		zap.Float64("score", score),
	).Debug("scored attestation data")

	return score
}

func (gc *GoClient) blockRootToSlot(root phase0.Root, logger *zap.Logger) (phase0.Slot, error) {
	slot, err, _ := gc.blockRootToSlotReqInflight.Do(root, func() (phase0.Slot, error) {
		cacheResult := gc.blockRootToSlotCache.Get(root)
		if cacheResult != nil {
			cachedSlot := cacheResult.Value()
			logger.
				With(zap.Uint64("cached_slot", uint64(cachedSlot))).
				With(zap.Int("cache_len", gc.blockRootToSlotCache.Len())).
				Debug("obtained slot from cache")
			return cachedSlot, nil
		}

		logger.Debug("slot was not found in cache, returning: '0'")

		return 0, nil
	})

	return slot, err
}

func weightedAttestationDataRequestIDField(id uuid.UUID) zap.Field {
	return zap.String("weighted_data_request_id", id.String())
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
