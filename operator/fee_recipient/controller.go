package fee_recipient

import (
	"context"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// ValidatorProvider provides access to validator shares
type ValidatorProvider interface {
	SelfValidators() []*types.SSVShare
}

// RecipientController submit proposal preparation to beacon node for all committee validators
type RecipientController interface {
	Start(ctx context.Context)
	FeeRecipientChangeChan() chan struct{}
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Ctx                context.Context
	BeaconClient       beaconprotocol.BeaconNode
	BeaconConfig       *networkconfig.BeaconConfig
	ValidatorProvider  ValidatorProvider
	RecipientStorage   storage.Recipients
	SlotTickerProvider slotticker.Provider
	OperatorDataStore  operatordatastore.OperatorDataStore
}

// recipientController implementation of RecipientController
type recipientController struct {
	logger               *zap.Logger
	ctx                  context.Context
	beaconClient         beaconprotocol.BeaconNode
	beaconConfig         *networkconfig.BeaconConfig
	validatorProvider    ValidatorProvider
	recipientStorage     storage.Recipients
	slotTickerProvider   slotticker.Provider
	operatorDataStore    operatordatastore.OperatorDataStore
	feeRecipientChangeCh chan struct{}
}

func NewController(logger *zap.Logger, opts *ControllerOptions) *recipientController {
	return &recipientController{
		logger:               logger,
		ctx:                  opts.Ctx,
		beaconClient:         opts.BeaconClient,
		beaconConfig:         opts.BeaconConfig,
		validatorProvider:    opts.ValidatorProvider,
		recipientStorage:     opts.RecipientStorage,
		slotTickerProvider:   opts.SlotTickerProvider,
		operatorDataStore:    opts.OperatorDataStore,
		feeRecipientChangeCh: make(chan struct{}, 1),
	}
}

func (rc *recipientController) Start(ctx context.Context) {
	go rc.listenToTicker(ctx)
	go rc.listenToFeeRecipientChanges(ctx)
}

// FeeRecipientChangeChan returns the channel used to notify about fee recipient changes
func (rc *recipientController) FeeRecipientChangeChan() chan struct{} {
	return rc.feeRecipientChangeCh
}

// GetProposalPreparations returns the proposal preparations for all validators
// This is used by the beacon client to re-submit on reconnect
func (rc *recipientController) GetProposalPreparations() ([]*eth2apiv1.ProposalPreparation, error) {
	return rc.getPreparations()
}

// getPreparations is a helper that fetches active validators and builds preparations
func (rc *recipientController) getPreparations() ([]*eth2apiv1.ProposalPreparation, error) {
	// Get current epoch for filtering
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

// listenToTicker submits proposal preparations periodically at the middle slot of each epoch.
// This ensures beacon nodes have current fee recipient information even if no changes occur.
// Event-driven updates are handled separately via listenToFeeRecipientChanges.
func (rc *recipientController) listenToTicker(ctx context.Context) {
	firstTimeSubmitted := false
	ticker := rc.slotTickerProvider()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			slot := ticker.Slot()
			// Check if this is the middle slot of the epoch
			if firstTimeSubmitted && uint64(slot)%rc.beaconConfig.SlotsPerEpoch != (rc.beaconConfig.SlotsPerEpoch/2) {
				continue
			}
			firstTimeSubmitted = true

			if err := rc.prepareAndSubmit(ctx); err != nil {
				rc.logger.Warn("could not submit proposal preparations", zap.Error(err))
			}
		}
	}
}

// listenToFeeRecipientChanges listens for fee recipient changes and submits preparations immediately
func (rc *recipientController) listenToFeeRecipientChanges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.feeRecipientChangeCh:
			rc.logger.Debug("fee recipient change detected, submitting proposal preparations")
			if err := rc.prepareAndSubmit(ctx); err != nil {
				rc.logger.Warn("could not submit proposal preparations after fee recipient change", zap.Error(err))
			}
		}
	}
}

func (rc *recipientController) prepareAndSubmit(ctx context.Context) error {
	preparations, err := rc.getPreparations()
	if err != nil {
		return fmt.Errorf("build preparations: %w", err)
	}

	if err = rc.beaconClient.SubmitProposalPreparations(ctx, preparations); err != nil {
		return fmt.Errorf("submit preparations: %w", err)
	}

	return nil
}

func (rc *recipientController) buildProposalPreparations(shares []*types.SSVShare) ([]*eth2apiv1.ProposalPreparation, error) {
	// build unique owners
	keys := make(map[common.Address]bool)
	var uniq []common.Address
	for _, entry := range shares {
		if _, value := keys[entry.OwnerAddress]; !value {
			keys[entry.OwnerAddress] = true
			uniq = append(uniq, entry.OwnerAddress)
		}
	}

	// get recipients
	rds, err := rc.recipientStorage.GetRecipientDataMany(nil, uniq)
	if err != nil {
		return nil, fmt.Errorf("could not get recipients data: %w", err)
	}

	// build proposal preparations
	preparations := make([]*eth2apiv1.ProposalPreparation, 0, len(shares))
	for _, share := range shares {
		feeRecipient, found := rds[share.OwnerAddress]
		if !found {
			copy(feeRecipient[:], share.OwnerAddress.Bytes())
		}
		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: share.ValidatorIndex,
			FeeRecipient:   feeRecipient,
		})
	}

	return preparations, nil
}
