package fee_recipient

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"go.uber.org/zap"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// RecipientController submit proposal preparation to beacon node for all committee validators
type RecipientController interface {
	Start()
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Logger            *zap.Logger
	Ctx               context.Context
	BeaconClient      beaconprotocol.Beacon
	EthNetwork        beaconprotocol.Network
	ShareStorage      validator.ICollection
	Ticker            slot_ticker.Ticker
	OperatorPublicKey string
}

// recipientController implementation of RecipientController
type recipientController struct {
	logger            *zap.Logger
	ctx               context.Context
	beaconClient      beaconprotocol.Beacon
	ethNetwork        beaconprotocol.Network
	shareStorage      validator.ICollection
	ticker            slot_ticker.Ticker
	operatorPublicKey string
}

func NewController(opts *ControllerOptions) RecipientController {
	return &recipientController{
		logger:            opts.Logger,
		ctx:               opts.Ctx,
		beaconClient:      opts.BeaconClient,
		ethNetwork:        opts.EthNetwork,
		shareStorage:      opts.ShareStorage,
		ticker:            opts.Ticker,
		operatorPublicKey: opts.OperatorPublicKey,
	}
}

func (rc *recipientController) Start() {
	tickerChan := make(chan phase0.Slot, 32)
	rc.ticker.Subscribe(tickerChan)
	rc.listenToTicker(tickerChan)
}

// listenToTicker loop over the given slot channel
func (rc *recipientController) listenToTicker(slots chan phase0.Slot) {
	firstTimeSubmitted := false
	for currentSlot := range slots {
		// submit if first time or if first slot in epoch
		slotsPerEpoch := rc.ethNetwork.SlotsPerEpoch()
		if firstTimeSubmitted && uint64(currentSlot)%slotsPerEpoch != 0 {
			continue
		}

		firstTimeSubmitted = true
		// submit fee recipient
		shares, err := rc.shareStorage.GetFilteredValidatorShares(validator.NotLiquidatedAndByOperatorPubKey(rc.operatorPublicKey))
		if err != nil {
			rc.logger.Warn("failed to get validators share", zap.Error(err))
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
						rc.logger.Warn("failed to create proposal preparation", zap.Error(err))
						continue
					}
					atomic.AddInt32(&counter, 1)
				}
				return rc.beaconClient.SubmitProposalPreparation(m)
			})
		}
		if err := g.Wait().ErrorOrNil(); err != nil {
			rc.logger.Warn("failed to submit proposal preparation", zap.Error(err))
		} else {
			rc.logger.Debug("proposal preparation submitted", zap.Int32("count", atomic.LoadInt32(&counter)))
		}
	}
}

func toProposalPreparation(m map[phase0.ValidatorIndex]bellatrix.ExecutionAddress, share *types.SSVShare) error {
	if share.HasBeaconMetadata() {
		var pubkey [20]byte
		pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(share.OwnerAddress, "0x"))
		if err != nil {
			return errors.Wrap(err, "failed to decode address")
		}
		copy(pubkey[:], pubKeyBytes)
		m[share.BeaconMetadata.Index] = pubkey
		return nil
	}
	return fmt.Errorf("missing meta data for pk %s", hex.EncodeToString(share.ValidatorPubKey))
}
