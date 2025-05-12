package goclient

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/google/uuid"
	"github.com/jellydator/ttlcache/v3"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
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
// Multiple calls for the same slot are joined into a single request, after which
// the result is cached for a short duration, deep copied and returned
func (gc *GoClient) GetAttestationData(slot phase0.Slot) (
	*phase0.AttestationData,
	spec.DataVersion,
	error,
) {
	// Have to make beacon node request and cache the result.
	result, err, _ := gc.attestationReqInflight.Do(slot, func() (*phase0.AttestationData, error) {
		// Check cache.
		cachedResult := gc.attestationDataCache.Get(slot)
		if cachedResult != nil {
			return cachedResult.Value(), nil
		}
		var (
			attestationData *phase0.AttestationData
			err             error
		)
		if gc.withWeightedAttestationData {
			attestationData, err = gc.weightedAttestationData(slot)
			if err != nil {
				return nil, err
			}
		} else {
			attestationData, err = gc.simpleAttestationData(slot)
			if err != nil {
				return nil, err
			}
		}

		// Caching resulting value here (as part of inflight request) guarantees only 1 request
		// will ever be done for a given slot.
		gc.attestationDataCache.Set(slot, attestationData, ttlcache.DefaultTTL)

		return attestationData, nil
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
				if bestAttestationData != nil {
					logger.Debug("updating best attestation data because of higher score",
						zap.String("client_addr", resp.clientAddr),
						zap.Float64("score", resp.score),
						fields.Root(resp.attestationData.BeaconBlockRoot),
					)
				}
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
					if bestAttestationData != nil {
						logger.Debug("updating best attestation data because of higher score",
							zap.String("client_addr", resp.clientAddr),
							zap.Float64("score", resp.score),
							fields.Root(resp.attestationData.BeaconBlockRoot),
						)
					}
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
		zap.Bool("with_weighted_attestation_data", false),
		zap.String("client_addr", gc.multiClient.Address()),
		fields.BlockRoot(resp.Data.BeaconBlockRoot),
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

	logger = logger.With(fields.BlockRoot(attestationData.BeaconBlockRoot))

	logger.Debug("scoring attestation data")
	score := gc.scoreAttestationData(ctx, client, attestationData, logger)

	respCh <- &attestationDataResponse{
		clientAddr:      addr,
		attestationData: attestationData,
		score:           score,
	}
}

// scoreAttestationData generates a score for attestation data.
// The score is relative to the reward expected from the contents of the attestation.
func (gc *GoClient) scoreAttestationData(ctx context.Context,
	client Client,
	attestationData *phase0.AttestationData,
	logger *zap.Logger,
) float64 {
	// Initial score is based on height of source and target epochs.
	score := float64(attestationData.Source.Epoch + attestationData.Target.Epoch)
	logger.
		With(zap.Float64("base_score", score)).
		Debug("base score was set. Fetching slot for block root")

	ctx, cancel := context.WithTimeout(ctx, gc.weightedAttestationDataSoftTimeout/2)
	defer cancel()

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()

	var (
		retries uint32
		start   = time.Now()
	)

	for {
		slot, err := gc.blockRootToSlot(ctx, client, attestationData.BeaconBlockRoot, logger)
		if err == nil {
			// Increase score based on the nearness of the head slot.
			denominator := float64(1 + attestationData.Slot - slot)
			if denominator > 0 {
				score += float64(1) / denominator
			} else {
				logger.
					With(zap.Float64("denominator", denominator)).
					Warn("denominator had unexpected value, score was not updated")
			}

			logger.With(
				zap.Duration("elapsed", time.Since(start)),
				zap.Uint64("head_slot", uint64(slot)),
				zap.Uint64("source_epoch", uint64(attestationData.Source.Epoch)),
				zap.Uint64("target_epoch", uint64(attestationData.Target.Epoch)),
				zap.Float64("score", score),
			).Debug("scored attestation data")

			return score
		}

		logger.
			With(zap.Error(err)).
			Warn("failed to obtain slot for block root")
		select {
		case <-ctx.Done():
			logger.
				With(zap.Uint32("try", retries)).
				With(zap.Duration("total_elapsed", time.Since(start))).
				Error("timeout for obtaining slot for block root was reached. Returning base score")
			return score
		case <-ticker.C:
			retries++
			logger.
				With(zap.Uint32("try", retries)).
				With(zap.Duration("total_elapsed", time.Since(start))).
				Warn("retrying to obtain slot for block root")
		}
	}
}

func (gc *GoClient) blockRootToSlot(ctx context.Context, client Client, root phase0.Root, logger *zap.Logger) (phase0.Slot, error) {
	cacheResult := gc.blockRootToSlotCache.Get(root)
	if cacheResult != nil {
		cachedSlot := cacheResult.Value()
		logger.
			With(zap.Uint64("cached_slot", uint64(cachedSlot))).
			With(zap.Int("cache_len", gc.blockRootToSlotCache.Len())).
			Debug("obtained slot from cache")
		return cachedSlot, nil
	}

	logger.Debug("slot was not found in cache, fetching from the client")

	timeoutContext, cancel := context.WithTimeout(ctx, gc.weightedAttestationDataSoftTimeout/4)
	defer cancel()

	blockResponse, err := client.BeaconBlockHeader(timeoutContext, &api.BeaconBlockHeaderOpts{
		Block: root.String(),
	})

	if err != nil {
		return 0, fmt.Errorf("failed to fetch block header from the client: %w", err)
	}

	if !isBlockHeaderResponseValid(blockResponse) {
		return 0, fmt.Errorf("block header response was not valid")
	}

	slot := blockResponse.Data.Header.Message.Slot
	gc.blockRootToSlotCache.Set(root, slot, ttlcache.NoTTL)
	logger.
		With(zap.Uint64("cached_slot", uint64(slot))).
		Debug("block root to slot cache updated from the BeaconBlockHeader call")

	return slot, nil
}

func isBlockHeaderResponseValid(response *api.Response[*eth2apiv1.BeaconBlockHeader]) bool {
	return response != nil && response.Data != nil && response.Data.Header != nil && response.Data.Header.Message != nil
}

func weightedAttestationDataRequestIDField(id uuid.UUID) zap.Field {
	return zap.String("weighted_data_request_id", id.String())
}

// multiClientSubmit is a generic function that submits data to multiple beacon clients concurrently.
// Returns nil if at least one client successfully submitted the data.
func (gc *GoClient) multiClientSubmit(
	operationName string,
	submitFunc func(ctx context.Context, client Client) error,
) error {
	logger := gc.log.With(zap.String("api", operationName))

	submissions := atomic.Int32{}
	p := pool.New().WithErrors().WithContext(gc.ctx).WithMaxGoroutines(len(gc.clients))
	for _, client := range gc.clients {
		client := client
		p.Go(func(ctx context.Context) error {
			clientAddress := client.Address()
			logger := logger.With(zap.String("client_addr", clientAddress))

			start := time.Now()
			err := submitFunc(ctx, client)
			recordRequestDuration(ctx, operationName, clientAddress, http.MethodPost, time.Since(start), err)
			if err != nil {
				logger.Debug("a client failed to submit",
					zap.Error(err))
				return fmt.Errorf("client %s failed to submit %s: %w", clientAddress, operationName, err)
			}

			logger.Debug("a client submitted successfully")

			submissions.Add(1)
			return nil
		})
	}
	err := p.Wait()
	if submissions.Load() > 0 {
		// At least one client has submitted successfully,
		// so we can return without error.
		return nil
	}
	if err != nil {
		logger.Error("all clients failed to submit",
			zap.Error(err))
		return fmt.Errorf("failed to submit %s", operationName)
	}
	return nil
}

// SubmitAttestations implements Beacon interface and sends attestations to the first client that succeeds
func (gc *GoClient) SubmitAttestations(attestations []*spec.VersionedAttestation) error {
	opts := &api.SubmitAttestationsOpts{Attestations: attestations}
	if gc.withParallelSubmissions {
		return gc.multiClientSubmit("SubmitAttestations", func(ctx context.Context, client Client) error {
			return client.SubmitAttestations(gc.ctx, opts)
		})
	}

	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitAttestations"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitAttestations(gc.ctx, opts)
	recordRequestDuration(gc.ctx, "SubmitAttestations", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted attestations")
	return nil
}
