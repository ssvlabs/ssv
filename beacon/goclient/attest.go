package goclient

import (
	"context"
	"errors"
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

const (
	// refetchDelay gives the BN time to update its cache after HeadEvent.
	// Value is a heuristic; monitor refetch_success vs refetch_still_mismatch.
	refetchDelay = 100 * time.Millisecond
	// refetchTimeout bounds re-fetch to preserve QBFT consensus budget.
	refetchTimeout = 500 * time.Millisecond
	// minTimeForRetry is minimum time needed before deadline to attempt retry.
	minTimeForRetry = refetchDelay + refetchTimeout
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
// the result is cached for a short duration, deep copied and returned.
// It also verifies the returned head against cached HeadEvent root and re-fetches if stale.
func (gc *GoClient) GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
	result, err, _ := gc.attestationReqInflight.Do(slot, func() (*phase0.AttestationData, error) {
		if cachedResult := gc.attestationDataCache.Get(slot); cachedResult != nil {
			return cachedResult.Value(), nil
		}

		attData, err := gc.fetchAttestationData(ctx, slot)
		if err != nil {
			return nil, err
		}

		attData, stale := gc.verifyAndRefetchIfStale(ctx, slot, attData)
		// Not caching stale data allows next caller to retry with fresh fetch.
		if !stale {
			gc.attestationDataCache.Set(slot, attData, ttlcache.DefaultTTL)
		}

		return attData, nil
	})
	if err != nil {
		return nil, DataVersionNil, err
	}

	return result, spec.DataVersionPhase0, nil
}

// fetchAttestationData fetches attestation data from beacon node(s).
func (gc *GoClient) fetchAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, error) {
	if gc.withWeightedAttestationData {
		return gc.weightedAttestationData(ctx, slot)
	}
	return gc.simpleAttestationData(ctx, slot)
}

// verifyAndRefetchIfStale checks attestation data against cached head root.
// If mismatch detected, waits briefly then re-fetches.
// Returns (attestationData, stale) where stale=true means data may be outdated.
func (gc *GoClient) verifyAndRefetchIfStale(
	ctx context.Context,
	slot phase0.Slot,
	attData *phase0.AttestationData,
) (result *phase0.AttestationData, stale bool) {
	attestationDataHeadVerifyCounter.Add(ctx, 1)

	item := gc.headCache.Get(slot)
	if item == nil {
		attestationDataHeadCacheMissCounter.Add(ctx, 1)
		return attData, true
	}
	expectedRoot := item.Value()

	if attData.BeaconBlockRoot == expectedRoot {
		attestationDataHeadMatchCounter.Add(ctx, 1)
		return attData, false
	}

	attestationDataHeadMismatchCounter.Add(ctx, 1)
	logger := gc.log.With(fields.Slot(slot))
	logger.Debug("attestation data head mismatch detected, will retry",
		zap.Stringer("expected_root", expectedRoot),
		zap.Stringer("got_root", attData.BeaconBlockRoot),
	)

	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) < minTimeForRetry {
			logger.Debug("not enough time remaining for retry",
				zap.Duration("remaining", time.Until(deadline)),
				zap.Duration("min_required", minTimeForRetry),
			)
			attestationDataRefetchSkippedCounter.Add(ctx, 1)
			return attData, true
		}
	}

	select {
	case <-ctx.Done():
		return attData, true
	case <-time.After(refetchDelay):
	}

	refetchCtx, cancel := context.WithTimeout(ctx, refetchTimeout)
	defer cancel()

	newAttData, err := gc.fetchAttestationDataFunc(refetchCtx, slot)
	if err != nil {
		logger.Warn("re-fetch failed, using original data", zap.Error(err))
		attestationDataRefetchFailedCounter.Add(ctx, 1)
		return attData, true
	}

	if newAttData.BeaconBlockRoot == expectedRoot {
		logger.Debug("re-fetch successful, got correct head")
		attestationDataRefetchSuccessCounter.Add(ctx, 1)
		return newAttData, false
	}

	logger.Warn("attestation data still mismatched after re-fetch",
		zap.Stringer("expected_root", expectedRoot),
		zap.Stringer("got_root", newAttData.BeaconBlockRoot),
	)
	attestationDataRefetchStillMismatchCounter.Add(ctx, 1)

	// Return re-fetched data as it's likely more recent.
	return newAttData, true
}

func (gc *GoClient) weightedAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot), weightedAttestationDataRequestIDField(uuid.New()))
	// We have two timeouts: a soft timeout and a hard timeout.
	// At the soft timeout, we return if we have any responses so far.
	// At the hard timeout, we return unconditionally.
	// The soft timeout is half the duration of the hard timeout.
	ctx, cancel := context.WithTimeout(ctx, gc.weightedAttestationDataHardTimeout)
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
		errs                error
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
			errs = errors.Join(errs, err.err)
			// A single client erroring is tolerated: the weighted fetch can still
			// succeed via another beacon node. Log per-client failures at debug and
			// aggregate them so the error returned on total failure reports why
			// each client failed.
			logger.With(
				zap.Duration("elapsed", time.Since(started)),
				zap.String("client_addr", err.clientAddr),
				zap.Int("succeeded", succeeded),
				zap.Int("errored", errored),
				zap.Error(err.err),
			).Debug("error fetching attestation data")
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
				errs = errors.Join(errs, err.err)
				logger.With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", err.clientAddr),
					zap.Int("succeeded", succeeded),
					zap.Int("errored", errored),
					zap.Error(err.err),
				).Debug("error fetching attestation data")
			case <-ctx.Done():
				hardTimedOut = numberOfRequests - (succeeded + errored)
				logger.With(
					zap.Duration("elapsed", time.Since(started)),
					zap.Int("succeeded", succeeded),
					zap.Int("errored", errored),
					zap.Int("hard_timed_out", hardTimedOut),
				).Warn("hard timeout reached")
			}
		}
	}

	if bestAttestationData == nil {
		if errs == nil {
			return nil, fmt.Errorf("all %d clients failed to get attestation data for slot %d", numberOfRequests, slot)
		}
		return nil, fmt.Errorf("all %d clients failed to get attestation data for slot %d, encountered errors: %w", numberOfRequests, slot, errs)
	}

	resultLogger := logger.With(
		zap.Duration("elapsed", time.Since(started)),
		zap.Int("succeeded", succeeded),
		zap.Int("errored", errored),
		zap.Int("soft_timed_out", softTimedOut),
		zap.Int("hard_timed_out", hardTimedOut),
		zap.Bool("with_weighted_attestation_data", true),
	)
	resultLogger.With(
		zap.String("client_addr", bestClientAddr),
		zap.Float64("score", bestScore)).
		Debug("successfully fetched attestation data")

	recordAttestationDataClientSelection(ctx, bestClientAddr)

	return bestAttestationData, nil
}

func shouldWaitForAttestationDataResponse(responded, errored, timedOut, requestsTotal int) bool {
	return responded+errored+timedOut != requestsTotal
}

func (gc *GoClient) simpleAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, error) {
	logger := gc.log.With(fields.Slot(slot))

	attDataReqStart := time.Now()
	resp, err := gc.multiClient.AttestationData(ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: 0,
	})
	recordRequest(ctx, logger, "AttestationData", gc.multiClient, http.MethodGet, true, time.Since(attDataReqStart), err)
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

	respCh <- &attestationDataResponse{
		clientAddr:      client.Address(),
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
			Warn("couldn't fetch slot for block root")
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
// Returns nil only if at least one client successfully submitted the data.
func (gc *GoClient) multiClientSubmit(
	ctx context.Context,
	routeName string,
	submitFunc func(ctx context.Context, client Client) error,
) error {
	if len(gc.clients) == 0 {
		// No clients to submit to. Guard explicitly: WithMaxGoroutines(0) below would panic, and
		// returning nil would break the "nil only on success" contract — callers such as the
		// attested-data-root cache would then treat a non-submission as a success.
		return errMultiClient(fmt.Errorf("no clients available to submit"), routeName)
	}
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
	// With at least one client, zero successful submissions means every client errored, so
	// p.Wait() returned a non-nil error.
	return errMultiClient(fmt.Errorf("all clients failed to submit: %w", err), routeName)
}

// SubmitAttestations implements Beacon interface and sends attestations to the first client that succeeds
func (gc *GoClient) SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error {
	opts := &api.SubmitAttestationsOpts{Attestations: attestations}
	if gc.withParallelSubmissions {
		if err := gc.multiClientSubmit(ctx, "SubmitAttestations", func(ctx context.Context, client Client) error {
			return client.SubmitAttestations(ctx, opts)
		}); err != nil {
			return err
		}
	} else {
		start := time.Now()
		err := gc.multiClient.SubmitAttestations(ctx, opts)
		recordRequest(ctx, gc.log, "SubmitAttestations", gc.multiClient, http.MethodPost, true, time.Since(start), err)
		if err != nil {
			return errMultiClient(fmt.Errorf("submit attestations: %w", err), "SubmitAttestations")
		}
	}

	// Only reached when at least one client accepted the attestations, so the beacon node
	// holds them — remember their data roots for the aggregator flow.
	gc.rememberAttestedDataRoots(ctx, attestations)
	return nil
}

// attestedDataRootKey identifies the attestation data a validator attested with; pre-Electra
// each committee signs different data (the committee is part of it as Index), so the
// committee belongs in the key.
type attestedDataRootKey struct {
	slot      phase0.Slot
	committee phase0.CommitteeIndex
}

// rememberAttestedDataRoots caches the root of the attestation data that was just submitted,
// per (slot, committee). The aggregator flow prefers this root over re-deriving the data:
// the beacon node holds at least our own attestation matching it, so an aggregate must
// exist, while a re-derived root may match nothing anyone attested with.
//
// The committee runner submits one attestation per validator, and validators in the same
// committee share a single AttestationData (for Electra it is identical across all committees),
// so we dedupe per distinct (slot, committee) to hash and store each root only once.
func (gc *GoClient) rememberAttestedDataRoots(ctx context.Context, attestations []*spec.VersionedAttestation) {
	seen := make(map[attestedDataRootKey]struct{}, len(attestations))
	for _, att := range attestations {
		data, err := att.Data()
		if err != nil {
			// We just submitted this attestation successfully, so extraction should not fail; a
			// silent miss would degrade the aggregator flow to the 404-prone re-derivation.
			gc.log.Warn("could not extract attestation data to remember its root", zap.Error(err))
			attestedDataRootRememberFailedCounter.Add(ctx, 1)
			continue
		}
		committee, err := att.CommitteeIndex()
		if err != nil {
			gc.log.Warn("could not extract attestation committee index to remember its root", zap.Error(err))
			attestedDataRootRememberFailedCounter.Add(ctx, 1)
			continue
		}
		key := attestedDataRootKey{slot: data.Slot, committee: committee}
		if _, ok := seen[key]; ok {
			continue
		}
		root, err := data.HashTreeRoot()
		if err != nil {
			gc.log.Warn("could not hash attestation data to remember its root", zap.Error(err))
			attestedDataRootRememberFailedCounter.Add(ctx, 1)
			continue
		}
		seen[key] = struct{}{}
		gc.attestedDataRootCache.Set(key, root, ttlcache.DefaultTTL)
	}
}

// attestedDataRoot returns the root of the attestation data this node submitted for the
// given slot and committee, if known.
func (gc *GoClient) attestedDataRoot(slot phase0.Slot, committee phase0.CommitteeIndex) (phase0.Root, bool) {
	item := gc.attestedDataRootCache.Get(attestedDataRootKey{slot: slot, committee: committee})
	if item == nil {
		return phase0.Root{}, false
	}
	return item.Value(), true
}
