package fee_recipient

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// ValidatorProvider provides access to validator shares and their fee recipients.
type ValidatorProvider interface {
	SelfValidators() []*types.SSVShare
	GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error)
}

// RecipientController submit proposal preparation to beacon node for all committee validators
type RecipientController interface {
	Start(ctx context.Context)
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Ctx                context.Context
	BeaconClient       beaconprotocol.BeaconNode
	BeaconConfig       *networkconfig.Beacon
	ValidatorProvider  ValidatorProvider
	SlotTickerProvider slotticker.Provider
	OperatorDataStore  operatordatastore.OperatorDataStore
}

// recipientController implementation of RecipientController
type recipientController struct {
	logger             *zap.Logger
	ctx                context.Context
	beaconClient       beaconprotocol.BeaconNode
	beaconConfig       *networkconfig.Beacon
	validatorProvider  ValidatorProvider
	slotTickerProvider slotticker.Provider
	operatorDataStore  operatordatastore.OperatorDataStore
}

func NewController(logger *zap.Logger, opts *ControllerOptions) *recipientController {
	return &recipientController{
		logger:             logger,
		ctx:                opts.Ctx,
		beaconClient:       opts.BeaconClient,
		beaconConfig:       opts.BeaconConfig,
		validatorProvider:  opts.ValidatorProvider,
		slotTickerProvider: opts.SlotTickerProvider,
		operatorDataStore:  opts.OperatorDataStore,
	}
}

func (rc *recipientController) Start(ctx context.Context) {
	rc.listenToTicker(ctx)
}

// getPreparations is a helper that fetches active validators and builds preparations
func (rc *recipientController) getPreparations() map[phase0.ValidatorIndex]bellatrix.ExecutionAddress {
	currentEpoch := rc.beaconConfig.EstimatedCurrentEpoch()

	// Filter validators that are actively attesting
	var activeShares []*types.SSVShare
	for _, share := range rc.validatorProvider.SelfValidators() {
		if share.IsAttesting(currentEpoch) {
			activeShares = append(activeShares, share)
		}
	}

	return rc.buildProposalPreparations(activeShares)
}

// listenToTicker submits proposal preparations on startup and then at the middle slot of each epoch.
func (rc *recipientController) listenToTicker(ctx context.Context) {
	firstTimeSubmitted := false
	ticker := rc.slotTickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			slot := ticker.Slot()
			// submit if first time or if first slot in epoch
			if firstTimeSubmitted && uint64(slot)%rc.beaconConfig.SlotsPerEpoch != (rc.beaconConfig.SlotsPerEpoch/2) {
				continue
			}
			firstTimeSubmitted = true

			rc.prepareAndSubmit(ctx)
		}
	}
}

func (rc *recipientController) prepareAndSubmit(ctx context.Context) {
	preparations := rc.getPreparations()

	// Convert map to slice of indices for batching
	indices := make([]phase0.ValidatorIndex, 0, len(preparations))
	for idx := range preparations {
		indices = append(indices, idx)
	}

	const batchSize = 500
	var submitted int
	for start := 0; start < len(indices); start += batchSize {
		end := start + batchSize
		if end > len(indices) {
			end = len(indices)
		}

		// Create batch map
		batch := make(map[phase0.ValidatorIndex]bellatrix.ExecutionAddress, end-start)
		for i := start; i < end; i++ {
			idx := indices[i]
			batch[idx] = preparations[idx]
		}

		err := rc.beaconClient.SubmitProposalPreparation(ctx, batch)
		if err != nil {
			rc.logger.Warn("could not submit proposal preparation batch",
				zap.Int("start_index", start),
				zap.Error(err),
			)
			continue
		}
		submitted += len(batch)
	}

	rc.logger.Debug("✅  successfully submitted proposal preparations",
		zap.Int("submitted", submitted),
		zap.Int("total", len(preparations)),
	)
}

func (rc *recipientController) buildProposalPreparations(shares []*types.SSVShare) map[phase0.ValidatorIndex]bellatrix.ExecutionAddress {
	preparations := make(map[phase0.ValidatorIndex]bellatrix.ExecutionAddress, len(shares))
	for _, share := range shares {
		feeRecipient, err := rc.validatorProvider.GetFeeRecipient(share.ValidatorPubKey)
		if err != nil {
			// Log the error and skip this validator
			rc.logger.Warn("could not get fee recipient for validator, skipping",
				zap.String("validator", fmt.Sprintf("%x", share.ValidatorPubKey)),
				zap.Error(err))
			continue
		}
		preparations[share.ValidatorIndex] = feeRecipient
	}

	return preparations
}
