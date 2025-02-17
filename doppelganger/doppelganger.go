package doppelganger

import (
	"context"
	"fmt"
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=doppelganger -destination=./doppelganger_mock.go -source=./doppelganger.go

// DefaultRemainingDetectionEpochs represents the initial number of epochs
// a validator must pass without liveness detection before being considered safe to sign.
const DefaultRemainingDetectionEpochs uint64 = 2

// PermanentlyUnsafe is a special flag value used to mark a validator as permanently unsafe for signing.
// It indicates that the validator was detected as live on another node and should not be trusted for signing.
const PermanentlyUnsafe = ^uint64(0)

type DoppelgangerProvider interface {
	ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus
	StartMonitoring(ctx context.Context)
	MarkAsSafe(validatorIndex phase0.ValidatorIndex)
	RemoveValidatorState(validatorIndex phase0.ValidatorIndex)
}

// ValidatorProvider represents a provider of validator information.
type ValidatorProvider interface {
	SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
}

type BeaconNode interface {
	ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ValidatorLiveness, error)
}

type DoppelgangerOptions struct {
	Network            networkconfig.NetworkConfig
	BeaconNode         BeaconNode
	ValidatorProvider  ValidatorProvider
	SlotTickerProvider slotticker.Provider
	Logger             *zap.Logger
}

type doppelgangerHandler struct {
	mu                 sync.RWMutex
	network            networkconfig.NetworkConfig
	beaconNode         BeaconNode
	validatorProvider  ValidatorProvider
	slotTickerProvider slotticker.Provider
	logger             *zap.Logger
	doppelgangerState  map[phase0.ValidatorIndex]*doppelgangerState

	startEpoch phase0.Epoch
}

// NewDoppelgangerHandler initializes a new instance of the Doppelgänger protection.
func NewDoppelgangerHandler(opts *DoppelgangerOptions) DoppelgangerProvider {
	return &doppelgangerHandler{
		network:            opts.Network,
		beaconNode:         opts.BeaconNode,
		validatorProvider:  opts.ValidatorProvider,
		slotTickerProvider: opts.SlotTickerProvider,
		logger:             opts.Logger.Named(logging.NameDoppelganger),
		doppelgangerState:  make(map[phase0.ValidatorIndex]*doppelgangerState),
	}
}

// ValidatorStatus returns the current status of the validator.
func (ds *doppelgangerHandler) ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	state, exists := ds.doppelgangerState[validatorIndex]
	if !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return UnknownToDoppelganger
	}

	if state.requiresFurtherChecks() {
		return SigningDisabled
	}
	return SigningEnabled
}

func (ds *doppelgangerHandler) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	state, exists := ds.doppelgangerState[validatorIndex]
	if !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	ds.logger.Debug("mark validator as doppelganger safe", fields.ValidatorIndex(validatorIndex))
	state.remainingEpochs = 0
}

func (ds *doppelgangerHandler) updateDoppelgangerState(validatorIndices []phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Create a set for current validators
	currentValidatorSet := make(map[phase0.ValidatorIndex]struct{}, len(validatorIndices))
	for _, idx := range validatorIndices {
		currentValidatorSet[idx] = struct{}{}
	}

	// Add new validators with DefaultRemainingDetectionEpochs
	for _, idx := range validatorIndices {
		if _, exists := ds.doppelgangerState[idx]; !exists {
			ds.doppelgangerState[idx] = &doppelgangerState{
				remainingEpochs: DefaultRemainingDetectionEpochs,
			}
			ds.logger.Debug("Added validator to Doppelganger state", fields.ValidatorIndex(idx))
		}
	}

	// Remove validators that are no longer in the current set
	for idx := range ds.doppelgangerState {
		if _, exists := currentValidatorSet[idx]; !exists {
			ds.logger.Debug("Removing validator from Doppelganger state", fields.ValidatorIndex(idx))
			delete(ds.doppelgangerState, idx)
		}
	}
}

func (ds *doppelgangerHandler) RemoveValidatorState(validatorIndex phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if _, exists := ds.doppelgangerState[validatorIndex]; !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		return
	}

	delete(ds.doppelgangerState, validatorIndex)
	ds.logger.Debug("Removed validator from Doppelganger state", fields.ValidatorIndex(validatorIndex))
}

func (ds *doppelgangerHandler) StartMonitoring(ctx context.Context) {
	ds.logger.Info("Doppelganger monitoring started")

	firstRun := true
	ticker := ds.slotTickerProvider()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			currentSlot := ticker.Slot()
			currentEpoch := ds.network.Beacon.EstimatedEpochAtSlot(currentSlot)
			slotsPerEpoch := ds.network.Beacon.SlotsPerEpoch()

			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, currentSlot%32+1)
			ds.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			// Update DG state with self participating validators from validator provider at the current epoch
			validatorIndices := indicesFromShares(ds.validatorProvider.SelfParticipatingValidators(currentEpoch))
			ds.updateDoppelgangerState(validatorIndices)

			// Perform liveness checks during the first run or at the last slot of the epoch
			if (!firstRun && uint64(currentSlot)%slotsPerEpoch != slotsPerEpoch-1) || ds.startEpoch == currentEpoch {
				continue
			}

			if firstRun {
				ds.startEpoch = currentEpoch
				firstRun = false
			}

			ds.checkLiveness(ctx, currentEpoch-1)
		}
	}
}

func (ds *doppelgangerHandler) checkLiveness(ctx context.Context, epoch phase0.Epoch) {
	ds.mu.Lock()
	validatorsToCheck := make([]phase0.ValidatorIndex, 0, len(ds.doppelgangerState))
	for validatorIndex, state := range ds.doppelgangerState {
		if state.requiresFurtherChecks() {
			validatorsToCheck = append(validatorsToCheck, validatorIndex)
		}
	}
	ds.mu.Unlock()

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

func (ds *doppelgangerHandler) processLivenessData(epoch phase0.Epoch, livenessData []*eth2apiv1.ValidatorLiveness) {
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
			ds.logger.Warn("Doppelganger detected for validator",
				fields.ValidatorIndex(response.Index),
				fields.Epoch(epoch),
			)
			state.remainingEpochs = PermanentlyUnsafe // Mark as permanently unsafe
			continue
		}

		// If the validator was previously marked as permanently unsafe (detected as live elsewhere),
		// but is now considered inactive, we reset the detection period to be safer.
		// Since we just checked for liveness now, we reduce the default detection period by 1
		// to ensure it gets checked once again in the next epoch before being marked safe.
		if state.remainingEpochs == PermanentlyUnsafe {
			state.remainingEpochs = DefaultRemainingDetectionEpochs - 1
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
