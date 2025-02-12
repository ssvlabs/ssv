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

//go:generate mockgen -package=mocks -destination=./mocks/service.go -source=./service.go

const DefaultRemainingDetectionEpochs uint64 = 2

type DoppelgangerProvider interface {
	ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus
	StartMonitoring(ctx context.Context)
	MarkAsSafe(validatorIndex phase0.ValidatorIndex)
}

// ValidatorProvider represents a provider of validator information.
type ValidatorProvider interface {
	SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
}

type ValidatorLiveness interface {
	ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ValidatorLiveness, error)
}

type DoppelgangerOptions struct {
	Network            networkconfig.NetworkConfig
	BeaconNode         ValidatorLiveness
	ValidatorProvider  ValidatorProvider
	SlotTickerProvider slotticker.Provider
	Logger             *zap.Logger
}

type DoppelgangerService struct {
	mu                 sync.RWMutex
	network            networkconfig.NetworkConfig
	beaconNode         ValidatorLiveness
	validatorProvider  ValidatorProvider
	slotTickerProvider slotticker.Provider
	logger             *zap.Logger
	doppelgangerState  map[phase0.ValidatorIndex]*DoppelgangerState

	startEpoch phase0.Epoch
}

// NewDoppelgangerService initializes a new instance of the Doppelganger protection service.
func NewDoppelgangerService(opts *DoppelgangerOptions) DoppelgangerProvider {
	return &DoppelgangerService{
		network:            opts.Network,
		beaconNode:         opts.BeaconNode,
		validatorProvider:  opts.ValidatorProvider,
		slotTickerProvider: opts.SlotTickerProvider,
		logger:             opts.Logger.Named(logging.NameDoppelganger),
		doppelgangerState:  make(map[phase0.ValidatorIndex]*DoppelgangerState),
	}
}

// ValidatorStatus returns the current status of the validator.
func (ds *DoppelgangerService) ValidatorStatus(validatorIndex phase0.ValidatorIndex) DoppelgangerStatus {
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

func (ds *DoppelgangerService) MarkAsSafe(validatorIndex phase0.ValidatorIndex) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	state, exists := ds.doppelgangerState[validatorIndex]
	if !exists {
		ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(validatorIndex))
		// TODO: edge case?
		return
	}

	ds.logger.Debug("mark validator as doppelganger safe", fields.ValidatorIndex(validatorIndex))
	state.RemainingEpochs = 0
}

func (ds *DoppelgangerService) updateDoppelgangerState(epoch phase0.Epoch) {
	validatorIndices := indicesFromShares(ds.validatorProvider.SelfParticipatingValidators(epoch))

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
			ds.doppelgangerState[idx] = &DoppelgangerState{
				RemainingEpochs: DefaultRemainingDetectionEpochs,
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

func (ds *DoppelgangerService) StartMonitoring(ctx context.Context) {
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
			ds.updateDoppelgangerState(currentEpoch)

			// Perform liveness check only on last slot of the epoch or first run .
			if (!firstRun && uint64(currentSlot)%slotsPerEpoch != slotsPerEpoch-1) || ds.startEpoch == currentEpoch {
				continue
			}

			if firstRun {
				ds.startEpoch = currentEpoch
				firstRun = false
			}

			ds.checkLiveness(ctx, currentEpoch)
		}
	}
}

func (ds *DoppelgangerService) checkLiveness(ctx context.Context, currentEpoch phase0.Epoch) {
	ds.logger.Info("Checking liveness for validators...")

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

	livenessEpoch := currentEpoch - 1
	livenessData, err := ds.beaconNode.ValidatorLiveness(ctx, livenessEpoch, validatorsToCheck)
	if err != nil {
		ds.logger.Error("Failed to obtain validator liveness data", zap.Error(err))
		// TODO(doppelganger): should we return an err here? edge case? do something based on error type?
		return
	}

	// Process liveness data
	ds.processLivenessData(livenessEpoch, livenessData)
}

func (ds *DoppelgangerService) processLivenessData(livenessEpoch phase0.Epoch, livenessData []*eth2apiv1.ValidatorLiveness) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	for _, response := range livenessData {
		ds.logger.Debug("Processing liveness data",
			fields.Epoch(livenessEpoch),
			fields.ValidatorIndex(response.Index),
			zap.Bool("is_live", response.IsLive))
		state, exists := ds.doppelgangerState[response.Index]
		if !exists {
			ds.logger.Warn("Validator not found in Doppelganger state", fields.ValidatorIndex(response.Index))
			// TODO: edge case?
			continue
		}

		if response.IsLive {
			ds.logger.Warn("Doppelganger detected for validator",
				fields.ValidatorIndex(response.Index),
				fields.Epoch(livenessEpoch),
			)
			state.RemainingEpochs = ^uint64(0) // Mark as permanently unsafe
			return
		}

		if state.RemainingEpochs == ^uint64(0) {
			ds.logger.Debug("Validator is no longer live, resetting to default detection epochs",
				fields.ValidatorIndex(response.Index),
			)
			state.RemainingEpochs = DefaultRemainingDetectionEpochs
			return
		}

		state.decreaseRemainingEpochs()
		if state.requiresFurtherChecks() {
			ds.logger.Debug("Validator still requires further checks",
				fields.ValidatorIndex(response.Index),
				zap.Uint64("remaining_epochs", state.RemainingEpochs))
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
