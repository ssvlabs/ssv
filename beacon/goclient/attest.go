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

	"github.com/ssvlabs/ssv/observability/log/fields"
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
	recordRequest(ctx, gc.log, "AttesterDuties", gc.multiClient, http.MethodPost, true, time.Since(start), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch attester duties: %w", err), "AttesterDuties")
	}
	if resp == nil {
		return nil, errMultiClient(fmt.Errorf("attester duties response is nil"), "AttesterDuties")
	}
	if resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("attester duties response data is nil"), "AttesterDuties")
	}

	return resp.Data, nil
}

// GetAttestationData returns attestation data for a given slot.
// Multiple calls for the same slot are joined into a single request, after which
// the result is cached for a short duration, deep copied and returned
func (gc *GoClient) GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
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
			attestationData, err = gc.weightedAttestationData(ctx, slot)
			if err != nil {
				return nil, err
			}
		} else {
			attestationData, err = gc.simpleAttestationData(ctx, slot)
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

func (gc *GoClient) weightedAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot), weightedAttestationDataRequestIDField(uuid.New()))

	started := time.Now()

	reqTotal := len(gc.clients)
	respCh := make(chan *attestationDataResponse, reqTotal)
	errCh := make(chan *attestationDataError, reqTotal)

	// reqCtx is used to control the lifetime of CL requests issued below.
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	for _, client := range gc.clients {
		go gc.fetchWeightedAttestationData(reqCtx, client, respCh, errCh, slot, logger)
	}

	// Wait for all responses and compare them, or if we hit soft deadline use the 1st response
	// that becomes available, or if we hit the hard-deadline (context done) just return.
	var (
		reqSucceeded, reqErrored, reqTimedOut int
		bestScore                             float64
		bestAttestationData                   *phase0.AttestationData
		bestClientAddr                        string
	)

	softDeadline := time.Unix(1<<63-62135596801, 999999999).UTC() // infinity
	hardDeadline, ok := ctx.Deadline()
	if ok {
		softDeadline = hardDeadline.Add(-300 * time.Millisecond)
	}

	onSuccess := func(resp *attestationDataResponse) {
		reqSucceeded++

		logger.With(
			fields.Took(time.Since(started)),
			zap.String("client_addr", resp.clientAddr),
		).Debug("fetch attestation data, successful response received")

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
	}
	onFailure := func(resp *attestationDataError) {
		reqErrored++

		logger.With(
			fields.Took(time.Since(started)),
			zap.String("client_addr", resp.clientAddr),
			zap.Error(resp.err),
		).Error("fetch attestation data, error received")
	}

	// Wait until all requests finish, or until we hit soft-deadline.
loopSoft:
	for haveAttestationDataReqInFlight(reqSucceeded, reqErrored, reqTotal) {
		select {
		case resp := <-respCh:
			onSuccess(resp)
		case resp := <-errCh:
			onFailure(resp)
		case <-time.After(time.Until(softDeadline)):
			break loopSoft
		}
	}

	// If we got some responses already - we'll use that, and cancel the rest inflight
	// requests. Otherwise, we'll keep waiting for the first success or until all requests
	// time out in the loop below.
	if reqSucceeded == 0 {
	loopHard:
		for haveAttestationDataReqInFlight(reqSucceeded, reqErrored, reqTotal) {
			select {
			case resp := <-respCh:
				onSuccess(resp)
				break loopHard // 1 success is good enough at this point
			case resp := <-errCh:
				onFailure(resp)
			}
		}
	}

	reqTimedOut = reqTotal - (reqSucceeded + reqErrored)

	logger.With(
		zap.Int("succeeded", reqSucceeded),
		zap.Int("errored", reqErrored),
		zap.Int("timed_out", reqTimedOut),
		zap.Bool("with_weighted_attestation_data", true),
		zap.String("client_addr", bestClientAddr),
		zap.Float64("best_score", bestScore),
		fields.Took(time.Since(started)),
	).Debug("done fetching weighted attestation data")

	if bestAttestationData == nil {
		return nil, fmt.Errorf("no attestations received")
	}

	recordAttestationDataClientSelection(ctx, bestClientAddr)

	return bestAttestationData, nil
}

func haveAttestationDataReqInFlight(responded, errored, requestsTotal int) bool {
	return responded+errored != requestsTotal
}

func (gc *GoClient) simpleAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot))

	reqStart := time.Now()
	resp, err := gc.multiClient.AttestationData(ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: 0,
	})
	recordRequest(ctx, logger, "AttestationData", gc.multiClient, http.MethodGet, true, time.Since(reqStart), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("get attestation data: %w", err), "AttestationData")
	}
	if resp == nil {
		return nil, errMultiClient(fmt.Errorf("attestation data response is nil"), "AttestationData")
	}
	if resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("attestation data is nil"), "AttestationData")
	}

	logger.Debug("successfully fetched attestation data",
		zap.Bool("with_weighted_attestation_data", false),
		fields.BlockRoot(resp.Data.BeaconBlockRoot),
	)
	recordAttestationDataClientSelection(ctx, gc.multiClient.Address())

	return resp.Data, nil
}

func (gc *GoClient) fetchWeightedAttestationData(
	ctx context.Context,
	client Client,
	respCh chan *attestationDataResponse,
	errCh chan *attestationDataError,
	slot phase0.Slot,
	logger *zap.Logger,
) {
	logger.Debug("fetching attestation data")

	reqStart := time.Now()
	response, err := client.AttestationData(ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: 0,
	})
	recordRequest(ctx, logger, "AttestationData", client, http.MethodGet, false, time.Since(reqStart), err)
	if err != nil {
		errCh <- &attestationDataError{
			clientAddr: client.Address(),
			err:        errSingleClient(fmt.Errorf("get attestation data: %w", err), client.Address(), "AttestationData"),
		}
		return
	}
	if response == nil {
		errCh <- &attestationDataError{
			clientAddr: client.Address(),
			err:        errSingleClient(fmt.Errorf("response is nil"), client.Address(), "AttestationData"),
		}
		return
	}
	attestationData := response.Data
	if attestationData == nil {
		errCh <- &attestationDataError{
			clientAddr: client.Address(),
			err:        errSingleClient(fmt.Errorf("response data nil"), client.Address(), "AttestationData"),
		}
		return
	}

	logger = logger.With(fields.BlockRoot(attestationData.BeaconBlockRoot))

	logger.Debug("scoring attestation data")
	score := gc.scoreAttestationData(ctx, client, attestationData, logger)
	logger.Debug("scoring attestation data, got score", zap.Float64("score", score))

	respCh <- &attestationDataResponse{
		clientAddr:      client.Address(),
		attestationData: attestationData,
		score:           score,
	}
}

// scoreAttestationData generates a score for attestation data. The score is relative to the reward expected from
// the contents of the attestation. This function retries multiple times in case the CL hasn't indexed the block
// with the corresponding root by the time we issue our 1st request.
func (gc *GoClient) scoreAttestationData(
	ctx context.Context,
	client Client,
	attestationData *phase0.AttestationData,
	logger *zap.Logger,
) float64 {
	// The initial score is based on the height of source and target epochs.
	score := float64(attestationData.Source.Epoch + attestationData.Target.Epoch)
	logger.
		With(zap.Float64("base_score", score)).
		Debug("base score was set. Fetching slot for block root")

	var (
		attempt = 1
		started = time.Now()
	)
	for attempt <= 5 {
		logger = logger.With(zap.Int("attempt", attempt))

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
				fields.Took(time.Since(started)),
				zap.Uint64("head_slot", uint64(slot)),
				zap.Uint64("source_epoch", uint64(attestationData.Source.Epoch)),
				zap.Uint64("target_epoch", uint64(attestationData.Target.Epoch)),
				zap.Float64("score", score),
			).Debug("scored attestation data")

			return score
		}

		logger.
			With(zap.Error(err)).
			Warn("couldn't fetch slot for block root")

		const retryDelay = 100 * time.Millisecond
		select {
		case <-ctx.Done():
			logger.
				With(fields.Took(time.Since(started))).
				Error("timeout for obtaining slot for block root was reached. Returning base score")
			return score
		case <-time.After(retryDelay):
			logger.
				With(fields.Took(time.Since(started))).
				Warn("retrying to obtain slot for block root")
			attempt++
			continue
		}
	}

	logger.
		With(fields.Took(time.Since(started))).
		Error("ran out of retries for obtaining slot for block root. Returning base score")
	return score
}

// blockRootToSlot looks up slot number by the provided block-root making a request to the provided CL client
// if necessary, caching the result for future re-use.
// Note, a block root "commits" (hashes over it) to a specific slot number - hence we can't accidentally
// update gc.blockRootToSlotCache here overwriting the freshest slot value with a different one regardless
// of whether blockRootToSlot is called concurrently. Hence, there is no need to worry about the atomicity of
// gc.blockRootToSlotCache updates, on the contrary - we shouldn't prevent concurrent blockRootToSlot calls
// for different clients from executing in parallel (such requests should execute in parallel so we can start
// using the results from those that finish faster than the rest of them).
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

	reqStart := time.Now()
	blockResponse, err := client.BeaconBlockHeader(ctx, &api.BeaconBlockHeaderOpts{
		Block: root.String(),
	})
	recordRequest(ctx, logger, "BeaconBlockHeader", client, http.MethodGet, false, time.Since(reqStart), err)
	if err != nil {
		return 0, errSingleClient(fmt.Errorf("failed to fetch block header from the client: %w", err), client.Address(), "BeaconBlockHeader")
	}
	if !isBlockHeaderResponseValid(blockResponse) {
		return 0, errSingleClient(fmt.Errorf("block header response was not valid"), client.Address(), "BeaconBlockHeader")
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
	ctx context.Context,
	routeName string,
	submitFunc func(ctx context.Context, client Client) error,
) error {
	submissions := atomic.Int32{}
	p := pool.New().WithErrors().WithContext(ctx).WithMaxGoroutines(len(gc.clients))
	for _, client := range gc.clients {
		p.Go(func(ctx context.Context) error {
			start := time.Now()
			err := submitFunc(ctx, client)
			recordRequest(ctx, gc.log, routeName, client, http.MethodPost, false, time.Since(start), err)
			if err != nil {
				return errSingleClient(fmt.Errorf("failed to submit: %w", err), client.Address(), routeName)
			}
			submissions.Add(1)
			return nil
		})
	}
	err := p.Wait()
	if submissions.Load() > 0 {
		// At least one client has submitted successfully, so we can return without error.
		return nil
	}
	if err != nil {
		return errMultiClient(fmt.Errorf("all clients failed to submit: %w", err), routeName)
	}
	return nil
}

// SubmitAttestations implements Beacon interface and sends attestations to the first client that succeeds
func (gc *GoClient) SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error {
	opts := &api.SubmitAttestationsOpts{Attestations: attestations}
	if gc.withParallelSubmissions {
		return gc.multiClientSubmit(ctx, "SubmitAttestations", func(ctx context.Context, client Client) error {
			return client.SubmitAttestations(ctx, opts)
		})
	}

	start := time.Now()
	err := gc.multiClient.SubmitAttestations(ctx, opts)
	recordRequest(ctx, gc.log, "SubmitAttestations", gc.multiClient, http.MethodPost, true, time.Since(start), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit attestations: %w", err), "SubmitAttestations")
	}

	return nil
}
