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

// GetAttestationData returns attestation data for a given slot and committeeIndex.
// Multiple calls for the same slot are joined into a single request, after which
// the result is cached for a short duration, deep copied and returned
// with the provided committeeIndex set.
func (gc *GoClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (
	*phase0.AttestationData,
	spec.DataVersion,
	error,
) {
	// Have to make beacon node request and cache the result.
	result, err, _ := gc.attestationReqInflight.Do(slot, func() (*phase0.AttestationData, error) {
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

	data, err := withCommitteeIndex(result, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to set committee index: %w", err)
	}

	return data, spec.DataVersionPhase0, nil
}

func (gc *GoClient) weightedAttestationData(slot phase0.Slot) (*phase0.AttestationData, error) {
	// We have two timeouts: a soft timeout and a hard timeout.
	// At the soft timeout, we return if we have any responses so far.
	// At the hard timeout, we return unconditionally.
	// The soft timeout is half the duration of the hard timeout.
	ctx, cancel := context.WithTimeout(gc.ctx, gc.commonTimeout)
	softCtx, softCancel := context.WithTimeout(ctx, gc.commonTimeout/2)
	started := time.Now()

	requests := len(gc.clients)
	respCh := make(chan *attestationDataResponse, requests)
	errCh := make(chan *attestationDataError, requests)

	for _, client := range gc.clients {
		go gc.fetchWeightedAttestationData(ctx, client, respCh, errCh, slot)
	}

	// Wait for all responses (or context done).
	var (
		responded,
		errored,
		timedOut,
		softTimedOut int
		bestScore           float64
		bestAttestationData *phase0.AttestationData
		bestClient          string
	)

	// Loop 1: prior to soft timeout.
	for responded+errored+timedOut+softTimedOut != requests {
		select {
		case resp := <-respCh:
			responded++
			gc.log.
				With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", resp.clientAddr),
					zap.Int("responded", responded),
					zap.Int("errored", errored),
					zap.Int("timed_out", timedOut),
				).Debug("response received")

			if bestAttestationData == nil || resp.score > bestScore {
				bestAttestationData = resp.attestationData
				bestScore = resp.score
				bestClient = resp.clientAddr
			}
		case err := <-errCh:
			errored++
			gc.log.
				With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", err.clientAddr),
					zap.Int("responded", responded),
					zap.Int("errored", errored),
					zap.Int("timed_out", timedOut),
					zap.Error(err.err),
				).Error("error received")
		case <-softCtx.Done():
			// If we have any responses at this point we consider the non-responders timed out.
			if responded > 0 {
				timedOut = requests - responded - errored
				gc.log.
					With(
						zap.Duration("elapsed", time.Since(started)),
						zap.Int("responded", responded),
						zap.Int("errored", errored),
						zap.Int("timed_out", timedOut),
					).Debug("soft timeout reached with responses")
			} else {
				gc.log.
					With(
						zap.Duration("elapsed", time.Since(started)),
						zap.Int("errored", errored),
					).Error("soft timeout reached with no responses")
			}
			// Set the number of requests that have soft timed out.
			softTimedOut = requests - responded - errored - timedOut
		}
	}
	softCancel()

	// Loop 2: after soft timeout.
	for responded+errored+timedOut != requests {
		select {
		case resp := <-respCh:
			responded++
			gc.log.
				With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", resp.clientAddr),
					zap.Int("responded", responded),
					zap.Int("errored", errored),
					zap.Int("timed_out", timedOut),
				).Debug("response received")
			if bestAttestationData == nil || resp.score > bestScore {
				bestAttestationData = resp.attestationData
				bestScore = resp.score
				bestClient = resp.clientAddr
			}
		case err := <-errCh:
			errored++
			gc.log.
				With(
					zap.Duration("elapsed", time.Since(started)),
					zap.String("client_addr", err.clientAddr),
					zap.Int("responded", responded),
					zap.Int("errored", errored),
					zap.Int("timed_out", timedOut),
					zap.Error(err.err),
				).Error("error received")
		case <-ctx.Done():
			// Anyone not responded by now is considered errored.
			timedOut = requests - responded - errored
			gc.log.
				With(
					zap.Duration("elapsed", time.Since(started)),
					zap.Int("responded", responded),
					zap.Int("errored", errored),
					zap.Int("timed_out", timedOut),
				).Error("hard timeout reached")
		}
	}
	cancel()

	logger := gc.log.With(
		zap.Duration("elapsed", time.Since(started)),
		zap.Int("responded", responded),
		zap.Int("errored", errored),
		zap.Int("timed_out", timedOut),
		zap.Bool("with_weighted_attestation_data", true),
	)
	if bestAttestationData == nil {
		logger.Error("no attestations received")
		return nil, fmt.Errorf("no attestations received")
	}

	logger.
		With(
			zap.String("client_addr", bestClient),
			zap.Float64("score", bestScore)).
		Debug("successfully fetched attestation data")

	return bestAttestationData, nil
}

func (gc *GoClient) simpleAttestationData(slot phase0.Slot) (*phase0.AttestationData, error) {
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

	gc.log.
		With(
			zap.Duration("elapsed", time.Since(attDataReqStart)),
			zap.Bool("with_weighted_attestation_data", false),
		).Debug("successfully fetched attestation data")

	return resp.Data, nil
}

func (gc *GoClient) fetchWeightedAttestationData(ctx context.Context,
	client Client,
	respCh chan *attestationDataResponse,
	errCh chan *attestationDataError,
	slot phase0.Slot,
) {
	addr := client.Address()
	attDataReqStart := time.Now()

	logger := gc.log.With(zap.String("client_addr", addr), fields.Slot(slot))

	logger.Debug("fetching attestation data")
	response, err := client.AttestationData(ctx, &api.AttestationDataOpts{
		Slot: slot,
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
	score := gc.scoreAttestationData(ctx, addr, attestationData)

	respCh <- &attestationDataResponse{
		clientAddr:      addr,
		attestationData: attestationData,
		score:           score,
	}
}

// scoreAttestationData generates a score for attestation data.
// The score is relative to the reward expected from the contents of the attestation.
func (gc *GoClient) scoreAttestationData(ctx context.Context,
	addr string,
	attestationData *phase0.AttestationData,
) float64 {
	logger := gc.log.With(
		fields.BlockRoot(attestationData.BeaconBlockRoot),
		zap.Uint64("attestation_data_slot", uint64(attestationData.Slot)),
		zap.String("client_addr", addr))
	// Initial score is based on height of source and target epochs.
	score := float64(attestationData.Source.Epoch + attestationData.Target.Epoch)

	// Increase score based on the nearness of the head slot.
	slot, err := gc.blockRootToSlot(ctx, attestationData.BeaconBlockRoot)
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

func (gc *GoClient) blockRootToSlot(ctx context.Context, root phase0.Root) (phase0.Slot, error) {
	logger := gc.log.With(fields.BlockRoot(root))
	cacheResult := gc.blockRootToSlotCache.Get(root)
	if cacheResult != nil {
		cachedSlot := cacheResult.Value()
		logger.
			With(fields.Slot(cachedSlot), zap.Int("cache_len", gc.blockRootToSlotCache.Len())).
			Debug("obtained slot from cache")
		return cachedSlot, nil
	}

	logger.Debug("slot was not found in cache. Fetching from Consensus Client")
	blockResponse, err := gc.multiClient.BeaconBlockHeader(ctx, &api.BeaconBlockHeaderOpts{
		Block: root.String(),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to obtain block header: %w", err)
	}

	fetchedSlot := blockResponse.Data.Header.Message.Slot
	logger.
		With(fields.Slot(fetchedSlot)).
		Debug("slot was fetched from Consensus Client")

	gc.blockRootToSlotCache.Set(root, fetchedSlot, ttlcache.NoTTL)

	return fetchedSlot, nil
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
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitAttestations"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitAttestations(gc.ctx, attestations)
	recordRequestDuration(gc.ctx, "SubmitAttestations", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted attestations")
	return nil
}
