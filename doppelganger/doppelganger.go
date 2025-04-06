package doppelganger

import (
	"context"
	"fmt"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=doppelganger -destination=./mock.go -source=./doppelganger.go

// initialRemainingDetectionEpochs represents the starting number of epochs
// a validator must pass without liveness detection before being considered safe to sign.
const initialRemainingDetectionEpochs phase0.Epoch = 2

type Provider interface {
	// Start begins the Doppelganger protection monitoring, periodically checking validator liveness.
	// Returns an error if the process fails to start or encounters a critical issue.
	Start(ctx context.Context) error

	// CanSign determines whether a validator is safe to sign based on Doppelganger protection status.
	// Returns true if the validator has passed all required safety checks, false otherwise.
	CanSign(validatorIndex phase0.ValidatorIndex) bool

	// ReportQuorum changes a validator's state to observed quorum, immediately bypassing further Doppelganger checks.
	// Typically used when a validator successfully completes post-consensus partial sig quorum (attester/proposer).
	ReportQuorum(validatorIndex phase0.ValidatorIndex)

	// RemoveValidatorState removes a validator from Doppelganger tracking, clearing its protection status.
	// Useful when a validator is no longer managed (validator removed or liquidated).
	RemoveValidatorState(validatorIndex phase0.ValidatorIndex)
}

// ValidatorProvider represents a provider of validator information.
type ValidatorProvider interface {
	SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
}

// BeaconNode represents a provider of beacon node data.
type BeaconNode interface {
	ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ValidatorLiveness, error)
}

// Options contains the configuration options for the Doppelgänger protection.
type Options struct {
	Network            networkconfig.NetworkConfig
	BeaconNode         BeaconNode
	ValidatorProvider  ValidatorProvider
	SlotTickerProvider slotticker.Provider
	Logger             *zap.Logger
}

// handler is the main struct for the Doppelgänger protection.
type handler struct {
	// mu synchronizes access to validatorsState
	mu              sync.RWMutex
	validatorsState map[phase0.ValidatorIndex]*doppelgangerState

	network            networkconfig.NetworkConfig
	beaconNode         BeaconNode
	validatorProvider  ValidatorProvider
	slotTickerProvider slotticker.Provider
	logger             *zap.Logger
}

// NewHandler initializes a new instance of the Doppelgänger protection.
func NewHandler(opts *Options) *handler {
	return &handler{
		network:            opts.Network,
		beaconNode:         opts.BeaconNode,
		validatorProvider:  opts.ValidatorProvider,
		slotTickerProvider: opts.SlotTickerProvider,
		logger:             opts.Logger.Named(logging.NameDoppelganger),
		validatorsState:    make(map[phase0.ValidatorIndex]*doppelgangerState),
	}
}

// CanSign returns true if the validator is safe to sign, otherwise false.
func (h *handler) CanSign(validatorIndex phase0.ValidatorIndex) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state := h.validatorsState[validatorIndex]
	if state == nil {
		h.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return false
	}

	return state.safe()
}

// ReportQuorum changes a validator's state to observed quorum, marking it as safe to sign in effect.
func (h *handler) ReportQuorum(validatorIndex phase0.ValidatorIndex) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.validatorsState[validatorIndex]
	if state == nil {
		h.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	if !state.safe() {
		state.observedQuorum = true
		h.logger.Info("Validator marked as safe due to observed quorum", fields.ValidatorIndex(validatorIndex))
	}
}

func (h *handler) updateDoppelgangerState(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// These slices store validator indices for logging purposes
	var addedValidators, removedValidators []uint64
	retrievedValidatorsSet := make(map[phase0.ValidatorIndex]struct{}, len(validatorIndices))

	// Add new validators with initialRemainingDetectionEpochs and build the retrieved set
	for _, idx := range validatorIndices {
		retrievedValidatorsSet[idx] = struct{}{}

		if h.validatorsState[idx] == nil {
			h.validatorsState[idx] = &doppelgangerState{
				remainingEpochs: initialRemainingDetectionEpochs,
			}
			addedValidators = append(addedValidators, uint64(idx))
		}
	}

	// Remove validators that are no longer in the retrieved set
	for idx := range h.validatorsState {
		if _, exists := retrievedValidatorsSet[idx]; !exists {
			removedValidators = append(removedValidators, uint64(idx))
			delete(h.validatorsState, idx)
		}
	}

	if len(addedValidators) > 0 {
		h.logger.Debug("Added validators to Doppelganger state", fields.Epoch(epoch), zap.Uint64s("validator_indices", addedValidators))
	}

	if len(removedValidators) > 0 {
		h.logger.Debug("Removed validators from Doppelganger state", fields.Epoch(epoch), zap.Uint64s("validator_indices", removedValidators))
	}
}

// RemoveValidatorState removes the validator from the Doppelganger state.
func (h *handler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.validatorsState[validatorIndex] == nil {
		h.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	delete(h.validatorsState, validatorIndex)
	h.logger.Debug("Removed validator from Doppelganger state", fields.ValidatorIndex(validatorIndex))
}

// Start starts the Doppelganger monitoring.
func (h *handler) Start(ctx context.Context) error {
	h.logger.Info("Doppelganger monitoring started")

	var startEpoch, previousEpoch phase0.Epoch
	firstRun := true
	ticker := h.slotTickerProvider()
	slotsPerEpoch := h.network.Beacon.SlotsPerEpoch()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(currentSlot)

			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, currentSlot%32+1)
			h.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			// Update DG state with self participating validators from validator provider at the current epoch
			validatorIndices := indicesFromShares(h.validatorProvider.SelfParticipatingValidators(currentEpoch))
			h.updateDoppelgangerState(currentEpoch, validatorIndices)

			// Perform liveness checks during the first run or at the last slot of the epoch.
			// This ensures that the beacon node has had enough time to observe blocks and attestations,
			// preventing delays in marking a validator as safe.
			if (!firstRun && uint64(currentSlot)%slotsPerEpoch != slotsPerEpoch-1) || startEpoch == currentEpoch {
				continue
			}

			if firstRun {
				startEpoch = currentEpoch
				firstRun = false
			}

			// Detect if an unexpected gap in epochs occurred (e.g., due to system sleep or clock drift).
			// If we detect a skipped epoch, we reset all doppelganger states to avoid unsafe signing.
			if previousEpoch > 0 && currentEpoch != previousEpoch+1 {
				h.logger.Warn("Epoch skipped unexpectedly, resetting all Doppelganger states",
					zap.Uint64("previous_epoch", uint64(previousEpoch)),
					zap.Uint64("current_epoch", uint64(currentEpoch)),
				)

				// Resetting all Doppelganger states ensures safety, but it also means
				// our operator will likely skip signing for at least a few epochs
				// or until there is a post-consensus partial sig quorum.
				h.resetDoppelgangerStates()
			}

			h.checkLiveness(ctx, currentSlot, currentEpoch-1)

			// Record the current count of safe and unsafe validators after each slot.
			// This ensures metrics reflect any changes from quorum reports, liveness updates, or state resets.
			h.recordValidatorStates(ctx)

			// Update the previous epoch tracker to detect potential future skips.
			previousEpoch = currentEpoch
		}
	}
}

func (h *handler) checkLiveness(ctx context.Context, slot phase0.Slot, epoch phase0.Epoch) {
	// Set a deadline until the start of the next slot, with a 100ms safety margin
	ctx, cancel := context.WithDeadline(ctx, h.network.Beacon.GetSlotStartTime(slot+1).Add(100*time.Millisecond))
	defer cancel()

	h.mu.RLock()
	validatorsToCheck := make([]phase0.ValidatorIndex, 0, len(h.validatorsState))
	for validatorIndex, state := range h.validatorsState {
		if !state.safe() {
			validatorsToCheck = append(validatorsToCheck, validatorIndex)
		}
	}
	h.mu.RUnlock()

	if len(validatorsToCheck) == 0 {
		h.logger.Debug("No validators require liveness check")
		return
	}

	livenessData, err := h.beaconNode.ValidatorLiveness(ctx, epoch, validatorsToCheck)
	if err != nil {
		h.logger.Error("Failed to obtain validator liveness data", zap.Error(err))
		return
	}

	// Process liveness data
	h.processLivenessData(epoch, livenessData)
}

func (h *handler) processLivenessData(epoch phase0.Epoch, livenessData []*eth2apiv1.ValidatorLiveness) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Debug("Processing liveness data", fields.Epoch(epoch), zap.Any("liveness_data", livenessData))

	for _, response := range livenessData {
		state := h.validatorsState[response.Index]
		if state == nil {
			h.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(response.Index))
			continue
		}

		if response.IsLive {
			h.logger.Warn("Doppelganger detected live validator",
				fields.ValidatorIndex(response.Index),
				fields.Epoch(epoch),
			)

			// Reset the validator's remaining epochs to the initial detection period,
			// ensuring it undergoes the full Doppelganger protection before being marked safe.
			state.resetRemainingEpochs()
			continue
		}

		// Log an error if decreaseRemainingEpochs fails, as it indicates an unexpected bug where
		// the function is called despite remainingEpochs already being at 0.
		if err := state.decreaseRemainingEpochs(); err != nil {
			h.logger.Error("Failed to decrease remaining epochs", zap.Error(err))
			continue
		}

		// Log if the validator still requires further checks
		if !state.safe() {
			h.logger.Debug("Validator still requires further checks",
				fields.ValidatorIndex(response.Index),
				zap.Uint64("remaining_epochs", uint64(state.remainingEpochs)))
		} else {
			h.logger.Debug("Validator is now safe to sign", fields.ValidatorIndex(response.Index))
		}
	}
}

// resetDoppelgangerStates resets all validator states back to the default remaining epochs.
func (h *handler) resetDoppelgangerStates() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, state := range h.validatorsState {
		state.resetRemainingEpochs()
	}

	h.logger.Info("All Doppelganger states reset to initial detection epochs")
}

func (h *handler) recordValidatorStates(ctx context.Context) {
	safe, unsafe := func() (safe, unsafe uint64) {
		h.mu.RLock()
		defer h.mu.RUnlock()

		for _, state := range h.validatorsState {
			if state.safe() {
				safe++
			} else {
				unsafe++
			}
		}
		return
	}()

	observability.RecordUint64Value(ctx, safe, validatorsStateGauge.Record, metric.WithAttributes(unsafeAttribute(false)))
	observability.RecordUint64Value(ctx, unsafe, validatorsStateGauge.Record, metric.WithAttributes(unsafeAttribute(true)))
}

func indicesFromShares(shares []*types.SSVShare) []phase0.ValidatorIndex {
	indices := make([]phase0.ValidatorIndex, len(shares))
	for i, share := range shares {
		indices[i] = share.ValidatorIndex
	}
	return indices
}
