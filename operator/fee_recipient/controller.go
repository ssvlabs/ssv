package fee_recipient

import (
	"context"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// ValidatorProvider provides access to validator shares
type ValidatorProvider interface {
	SelfValidators() []*types.SSVShare
	GetFeeRecipient(validatorPK spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error)
}

// RecipientController submit proposal preparation to beacon node for all committee validators
type RecipientController interface {
	Start(ctx context.Context)
	SubscribeToFeeRecipientChanges(ch <-chan struct{})
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
	logger               *zap.Logger
	ctx                  context.Context
	beaconClient         beaconprotocol.BeaconNode
	beaconConfig         *networkconfig.Beacon
	validatorProvider    ValidatorProvider
	slotTickerProvider   slotticker.Provider
	operatorDataStore    operatordatastore.OperatorDataStore
	feeRecipientChangeCh <-chan struct{}
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
	go rc.submitPreparationsOnSchedule(ctx)
	go rc.submitPreparationsOnChange(ctx)
}

// SubscribeToFeeRecipientChanges subscribes to fee recipient change notifications from ValidatorController
func (rc *recipientController) SubscribeToFeeRecipientChanges(ch <-chan struct{}) {
	rc.feeRecipientChangeCh = ch
}

// GetProposalPreparations returns the proposal preparations for all validators
// This is used by the beacon client to re-submit on reconnect
func (rc *recipientController) GetProposalPreparations() ([]*eth2apiv1.ProposalPreparation, error) {
	return rc.getPreparations()
}

// getPreparations is a helper that fetches active validators and builds preparations
func (rc *recipientController) getPreparations() ([]*eth2apiv1.ProposalPreparation, error) {
	currentEpoch := rc.beaconConfig.EstimatedCurrentEpoch()

	var activeShares []*types.SSVShare
	for _, share := range rc.validatorProvider.SelfValidators() {
		if share.IsAttesting(currentEpoch) {
			activeShares = append(activeShares, share)
		}
	}

	return rc.buildProposalPreparations(activeShares)
}

// submitPreparationsOnSchedule submits proposal preparations periodically at the middle slot of each epoch.
// This ensures beacon nodes have current fee recipient information even if no changes occur.
// Event-driven updates are handled separately via submitPreparationsOnChange.
func (rc *recipientController) submitPreparationsOnSchedule(ctx context.Context) {
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

// submitPreparationsOnChange listens for fee recipient changes and submits preparations immediately
func (rc *recipientController) submitPreparationsOnChange(ctx context.Context) {
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
	preparations := make([]*eth2apiv1.ProposalPreparation, 0, len(shares))

	for _, share := range shares {
		feeRecipient, err := rc.validatorProvider.GetFeeRecipient(share.ValidatorPubKey)
		if err != nil {
			rc.logger.Warn("could not get fee recipient for validator, skipping",
				zap.String("validator", fmt.Sprintf("%x", share.ValidatorPubKey)),
				zap.Error(err))
			continue
		}

		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: share.ValidatorIndex,
			FeeRecipient:   feeRecipient,
		})
	}

	return preparations, nil
}
