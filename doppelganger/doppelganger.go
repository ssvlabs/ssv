package doppelganger

import (
	"context"
	"fmt"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=doppelganger -destination=./mock.go -source=./doppelganger.go

// defaultRemainingDetectionEpochs represents the initial number of epochs
// a validator must pass without liveness detection before being considered safe to sign.
const defaultRemainingDetectionEpochs uint64 = 2

// permanentlyUnsafe is a special flag value used to mark a validator as permanently unsafe for signing.
// It indicates that the validator was detected as live on another node and should not be trusted for signing.
const permanentlyUnsafe = ^uint64(0)

type Provider interface {
	// Start begins the Doppelganger protection monitoring, periodically checking validator liveness.
	// Returns an error if the process fails to start or encounters a critical issue.
	Start(ctx context.Context) error

	// CanSign determines whether a validator is safe to sign based on Doppelganger protection status.
	// Returns true if the validator has passed all required safety checks, false otherwise.
	CanSign(validatorIndex phase0.ValidatorIndex) bool

	// MarkAsSafe marks a validator as safe for signing, immediately bypassing further Doppelganger checks.
	// Typically used when a validator successfully completes post-consensus partial sig quorum (attester/proposer).
	MarkAsSafe(validatorIndex phase0.ValidatorIndex)

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

// Handler is the main struct for the Doppelgänger protection.
type Handler struct {
	// mu synchronizes access to doppelgangerState
	mu                sync.RWMutex
	doppelgangerState map[phase0.ValidatorIndex]*doppelgangerState

	network            networkconfig.NetworkConfig
	beaconNode         BeaconNode
	validatorProvider  ValidatorProvider
	slotTickerProvider slotticker.Provider
	logger             *zap.Logger

	startEpoch phase0.Epoch
}

// NewHandler initializes a new instance of the Doppelgänger protection.
func NewHandler(opts *Options) *Handler {
	return &Handler{
		network:            opts.Network,
		beaconNode:         opts.BeaconNode,
		validatorProvider:  opts.ValidatorProvider,
		slotTickerProvider: opts.SlotTickerProvider,
		logger:             opts.Logger.Named(logging.NameDoppelganger),
		doppelgangerState:  make(map[phase0.ValidatorIndex]*doppelgangerState),
	}
}

// CanSign returns true if the validator is safe to sign, otherwise false.
func (ds *Handler) CanSign(validatorIndex phase0.ValidatorIndex) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	state, exists := ds.doppelgangerState[validatorIndex]
	if !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return false
	}

	return !state.requiresFurtherChecks()
}

// MarkAsSafe marks the validator as safe to sign.
func (ds *Handler) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	state, exists := ds.doppelgangerState[validatorIndex]
	if !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	if state.requiresFurtherChecks() {
		state.remainingEpochs = 0
		ds.logger.Info("Validator marked as safe", fields.ValidatorIndex(validatorIndex))
	}
}

func (ds *Handler) updateDoppelgangerState(validatorIndices []phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// These slices store validator indices for logging purposes
	var addedValidators, removedValidators []uint64

	// Add new validators with defaultRemainingDetectionEpochs
	for _, idx := range validatorIndices {
		if _, exists := ds.doppelgangerState[idx]; !exists {
			ds.doppelgangerState[idx] = &doppelgangerState{
				remainingEpochs: defaultRemainingDetectionEpochs,
			}
			addedValidators = append(addedValidators, uint64(idx))
		}
	}

	// Create a set for current validators
	currentValidatorSet := make(map[phase0.ValidatorIndex]struct{}, len(validatorIndices))
	for _, idx := range validatorIndices {
		currentValidatorSet[idx] = struct{}{}
	}

	// Remove validators that are no longer in the current set
	for idx := range ds.doppelgangerState {
		if _, exists := currentValidatorSet[idx]; !exists {
			removedValidators = append(removedValidators, uint64(idx))
			delete(ds.doppelgangerState, idx)
		}
	}

	if len(addedValidators) > 0 {
		ds.logger.Debug("Added validators to Doppelganger state", zap.Uint64s("validator_indices", addedValidators))
	}

	if len(removedValidators) > 0 {
		ds.logger.Debug("Removed validators from Doppelganger state", zap.Uint64s("validator_indices", removedValidators))
	}
}

// RemoveValidatorState removes the validator from the Doppelganger state.
func (ds *Handler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if _, exists := ds.doppelgangerState[validatorIndex]; !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	delete(ds.doppelgangerState, validatorIndex)
	ds.logger.Debug("Removed validator from Doppelganger state", fields.ValidatorIndex(validatorIndex))
}

// Start starts the Doppelganger monitoring.
func (ds *Handler) Start(ctx context.Context) error {
	ds.logger.Info("Doppelganger monitoring started")

	firstRun := true
	ticker := ds.slotTickerProvider()
	slotsPerEpoch := ds.network.Beacon.SlotsPerEpoch()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			currentEpoch := ds.network.Beacon.EstimatedEpochAtSlot(currentSlot)

			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, currentSlot%32+1)
			ds.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			// Update DG state with self participating validators from validator provider at the current epoch
			validatorIndices := indicesFromShares(ds.validatorProvider.SelfParticipatingValidators(currentEpoch))
			ds.updateDoppelgangerState(validatorIndices)

			// Perform liveness checks during the first run or at the last slot of the epoch.
			// This ensures that the beacon node has had enough time to observe blocks and attestations,
			// preventing delays in marking a validator as safe.
			if (!firstRun && uint64(currentSlot)%slotsPerEpoch != slotsPerEpoch-1) || ds.startEpoch == currentEpoch {
				continue
			}

			if firstRun {
				ds.startEpoch = currentEpoch
				firstRun = false
			}

			ds.checkLiveness(ctx, currentSlot, currentEpoch-1)
		}
	}
}

func (ds *Handler) checkLiveness(ctx context.Context, slot phase0.Slot, epoch phase0.Epoch) {
	// Set a deadline until the start of the next slot, with a 100ms safety margin
	ctx, cancel := context.WithDeadline(ctx, ds.network.Beacon.GetSlotStartTime(slot+1).Add(100*time.Millisecond))
	defer cancel()

	ds.mu.RLock()
	validatorsToCheck := make([]phase0.ValidatorIndex, 0, len(ds.doppelgangerState))
	for validatorIndex, state := range ds.doppelgangerState {
		if state.requiresFurtherChecks() {
			validatorsToCheck = append(validatorsToCheck, validatorIndex)
		}
	}
	ds.mu.RUnlock()

	if len(validatorsToCheck) == 0 {
		ds.logger.Debug("No validators require liveness check")
		return
	}

	livenessData, err := ds.beaconNode.ValidatorLiveness(ctx, epoch, validatorsToCheck)
	if err != nil {
		ds.logger.Error("Failed to obtain validator liveness data", zap.Error(err))
		return
	}

	// Process liveness data
	ds.processLivenessData(epoch, livenessData)
}

func (ds *Handler) processLivenessData(epoch phase0.Epoch, livenessData []*eth2apiv1.ValidatorLiveness) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	for _, response := range livenessData {
		ds.logger.Debug("Processing liveness data",
			fields.Epoch(epoch),
			fields.ValidatorIndex(response.Index),
			zap.Bool("is_live", response.IsLive))
		state, exists := ds.doppelgangerState[response.Index]
		if !exists {
			ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(response.Index))
			continue
		}

		if response.IsLive {
			ds.logger.Warn("Doppelganger detected live validator",
				fields.ValidatorIndex(response.Index),
				fields.Epoch(epoch),
			)
			state.remainingEpochs = permanentlyUnsafe // Mark as permanently unsafe
			continue
		}

		// If the validator was previously marked as permanently unsafe (detected as live elsewhere),
		// but is now considered inactive, we reset the detection period to be safer.
		// Since we just checked for liveness now, we reduce the default detection period by 1
		// to ensure it gets checked once again in the next epoch before being marked safe.
		if state.permanentlyUnsafe() {
			state.remainingEpochs = defaultRemainingDetectionEpochs - 1
			ds.logger.Debug("Validator is no longer live but requires further checks",
				fields.ValidatorIndex(response.Index),
				zap.Uint64("remaining_epochs", state.remainingEpochs))
			continue
		}

		state.decreaseRemainingEpochs()
		if state.requiresFurtherChecks() {
			ds.logger.Debug("Validator still requires further checks",
				fields.ValidatorIndex(response.Index),
				zap.Uint64("remaining_epochs", state.remainingEpochs))
		} else {
			ds.logger.Debug("Validator is now safe to sign", fields.ValidatorIndex(response.Index))
		}
	}
}

func indicesFromShares(shares []*types.SSVShare) []phase0.ValidatorIndex {
	indices := make([]phase0.ValidatorIndex, len(shares))
	for i, share := range shares {
		indices[i] = share.ValidatorIndex
	}
	return indices
}
