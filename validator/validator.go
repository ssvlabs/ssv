package validator

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/collections"
)

// Options to add in validator struct creation
type Options struct {
	SignatureCollectionTimeout time.Duration
	SlotQueue                  slotqueue.Queue
}

// Validator struct that manages all ibft wrappers
type Validator struct {
	ctx                        context.Context
	logger                     *zap.Logger
	ValidatorShare             *collections.ValidatorShare
	ibftStorage                collections.Iibft
	ethNetwork                 core.Network
	network                    network.Network
	beacon                     beacon.Beacon
	ibfts                      map[beacon.Role]ibft.IBFT
	msgQueue                   *msgqueue.MessageQueue
	slotQueue                  slotqueue.Queue
	SignatureCollectionTimeout time.Duration
}

// New Validator creation
func New(ctx context.Context, logger *zap.Logger, validatorShare *collections.ValidatorShare, ibftStorage collections.Iibft, network network.Network, ethNetwork core.Network, _beacon beacon.Beacon, opt Options) *Validator {
	logger = logger.
		With(zap.String("pubKey", validatorShare.ValidatorPK.SerializeToHexStr())).
		With(zap.Uint64("node_id", validatorShare.NodeID))
	msgQueue := msgqueue.New()
	ibfts := make(map[beacon.Role]ibft.IBFT)
	ibfts[beacon.RoleAttester] = ibft.New(
		logger,
		ibftStorage,
		network,
		msgQueue,
		&proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   validatorShare.Committee,
		},
		validatorShare,
	)
	go ibfts[beacon.RoleAttester].Init()

	return &Validator{
		ctx:                        ctx,
		logger:                     logger,
		ValidatorShare:             validatorShare,
		ibftStorage:                ibftStorage,
		ethNetwork:                 ethNetwork,
		network:                    network,
		beacon:                     _beacon,
		ibfts:                      ibfts,
		msgQueue:                   msgQueue,
		slotQueue:                  opt.SlotQueue,
		SignatureCollectionTimeout: opt.SignatureCollectionTimeout,
	}
}

// Start validator
func (v *Validator) Start() error {
	if err := v.network.SubscribeToValidatorNetwork(v.ValidatorShare.ValidatorPK); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}
	go v.startSlotQueueListener()
	go v.listenToNetworkMessages()
	return nil
}

// startSlotQueueListener starts slot queue listener
func (v *Validator) startSlotQueueListener() {
	v.logger.Info("start listening slot queue")

	for {
		slot, duty, ok, err := v.slotQueue.Next(v.ValidatorShare.ValidatorPK.Serialize())
		if err != nil {
			v.logger.Error("failed to get next slot data", zap.Error(err))
			continue
		}

		if !ok {
			v.logger.Debug("no duties for slot scheduled")
			continue
		}
		go v.ExecuteDuty(v.ctx, slot, duty)
	}
}

func (v *Validator) listenToNetworkMessages() {
	sigChan := v.network.ReceivedSignatureChan()
	for sigMsg := range sigChan {
		v.msgQueue.AddMessage(&network.Message{
			Lambda:        sigMsg.Message.Lambda,
			SignedMessage: sigMsg,
			Type:          network.NetworkMsg_SignatureType,
		})
	}
}

// getSlotStartTime returns the start time for the given slot  TODO: redundant func (in ssvNode) need to fix
func (v *Validator) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(v.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(v.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}
