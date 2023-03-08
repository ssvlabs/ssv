package fee_recipient

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/registry/storage"

	"github.com/hashicorp/go-multierror"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// RecipientController submit proposal preparation to beacon node for all committee validators
type RecipientController interface {
	Start(logger *zap.Logger)
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Ctx          context.Context
	BeaconClient beaconprotocol.Beacon
	EthNetwork   beaconprotocol.Network
	ShareStorage validator.ICollection
	Ticker       slot_ticker.Ticker
	OperatorData *storage.OperatorData
}

// recipientController implementation of RecipientController
type recipientController struct {
	ctx          context.Context
	beaconClient beaconprotocol.Beacon
	ethNetwork   beaconprotocol.Network
	shareStorage validator.ICollection
	ticker       slot_ticker.Ticker
	operatorData *storage.OperatorData
}

func NewController(opts *ControllerOptions) *recipientController {
	return &recipientController{

		ctx:          opts.Ctx,
		beaconClient: opts.BeaconClient,
		ethNetwork:   opts.EthNetwork,
		shareStorage: opts.ShareStorage,
		ticker:       opts.Ticker,
		operatorData: opts.OperatorData,
	}
}

func (rc *recipientController) Start(logger *zap.Logger) {
	tickerChan := make(chan phase0.Slot, 32)
	rc.ticker.Subscribe(tickerChan)
	rc.listenToTicker(logger, tickerChan)
}

// listenToTicker loop over the given slot channel
func (rc *recipientController) listenToTicker(logger *zap.Logger, slots chan phase0.Slot) {
	firstTimeSubmitted := false
	for currentSlot := range slots {
		// submit if first time or if first slot in epoch
		slotsPerEpoch := rc.ethNetwork.SlotsPerEpoch()
		if firstTimeSubmitted && uint64(currentSlot)%slotsPerEpoch != 0 {
			continue
		}

		firstTimeSubmitted = true
		// submit fee recipient
		shares, err := rc.shareStorage.GetFilteredValidatorShares(logger, validator.ByOperatorIDAndNotLiquidated(rc.operatorData.ID))
		if err != nil {
			logger.Warn("failed to get validators share", zap.Error(err))
			continue
		}

		var g multierror.Group
		const batchSize = 500
		var counter int32
		for start, end := 0, 0; start <= len(shares)-1; start = end {
			end = start + batchSize
			if end > len(shares) {
				end = len(shares)
			}
			batch := shares[start:end]

			g.Go(func() error {
				m := make(map[phase0.ValidatorIndex]bellatrix.ExecutionAddress)
				for _, share := range batch {
					if err := toProposalPreparation(m, share); err != nil {
						logger.Warn("failed to create proposal preparation", zap.Error(err))
						continue
					}
					atomic.AddInt32(&counter, 1)
				}
				return rc.beaconClient.SubmitProposalPreparation(m)
			})
		}
		if err := g.Wait().ErrorOrNil(); err != nil {
			logger.Warn("failed to submit proposal preparation", zap.Error(err))
		} else {
			logger.Debug("proposal preparation submitted", zap.Int32("count", atomic.LoadInt32(&counter)))
		}
	}
}

func toProposalPreparation(m map[phase0.ValidatorIndex]bellatrix.ExecutionAddress, share *types.SSVShare) error {
	if share.HasBeaconMetadata() {
		var execAddress bellatrix.ExecutionAddress
		copy(execAddress[:], share.OwnerAddress.Bytes())
		m[share.BeaconMetadata.Index] = execAddress
		return nil
	}
	return fmt.Errorf("missing meta data for pk %s", hex.EncodeToString(share.ValidatorPubKey))
}
