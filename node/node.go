package node

import (
	"context"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/collections"
)

// Options contains options to create the node
type Options struct {
	ValidatorStorage           collections.ValidatorStorage
	ETHNetwork                 core.Network
	Network                    network.Network
	Queue                      *msgqueue.MessageQueue
	Consensus                  string
	Beacon                     beacon.Beacon
	IBFT                       ibft.IBFT
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration
	Phase1TestGenesis          uint64
}

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start(ctx context.Context) error
	// Update updates observer
	Update(i interface{})
	// GetID get the observer id
	GetID() string
}

// ssvNode implements Node interface
type ssvNode struct {
	validatorStorage collections.ValidatorStorage
	ethNetwork       core.Network
	network          network.Network
	queue            *msgqueue.MessageQueue
	consensus        string
	slotQueue        slotqueue.Queue
	beacon           beacon.Beacon
	iBFT             ibft.IBFT
	logger           *zap.Logger

	// timeouts
	signatureCollectionTimeout time.Duration
	// genesis epoch
	phase1TestGenesis uint64
	pubsub.BaseObserver
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	return &ssvNode{
		validatorStorage:           opts.ValidatorStorage,
		ethNetwork:                 opts.ETHNetwork,
		network:                    opts.Network,
		queue:                      opts.Queue,
		consensus:                  opts.Consensus,
		slotQueue:                  slotqueue.New(opts.ETHNetwork),
		beacon:                     opts.Beacon,
		iBFT:                       opts.IBFT,
		logger:                     opts.Logger,
		signatureCollectionTimeout: opts.SignatureCollectionTimeout,
		// genesis epoch
		phase1TestGenesis: opts.Phase1TestGenesis,
	}
}

// Update updates observer
func (n *ssvNode) Update(vLog interface{}) {
	n.logger.Info("Got log from contract", zap.Any("log", vLog)) // pointer to Event log
}

// GetID get the observer id
func (n *ssvNode) GetID() string {
	// TODO return proper id for the observer
	return "ssvNode"
}

// Start implements Node interface
func (n *ssvNode) Start(ctx context.Context) error {
	validators, err := n.validatorStorage.GetAllValidatorsShare()
	validator := validators[0] // TODO: temp getting the :0 from slice. need to support multi valShare
	if err != nil {
		n.logger.Fatal("Failed to get validatorStorage share", zap.Error(err))
	}

	go n.startSlotQueueListener(ctx, validator.PubKey)
	go n.listenToNetworkMessages()

	streamDuties, err := n.beacon.StreamDuties(ctx, validator.PubKey.Serialize())
	if err != nil {
		n.logger.Error("failed to open duties stream", zap.Error(err))
	}

	n.logger.Info("start streaming duties")
	for duty := range streamDuties {
		go func(duty *ethpb.DutiesResponse_Duty) {
			slots := collectSlots(duty)
			if len(slots) == 0 {
				n.logger.Debug("no slots found for the given duty")
				return
			}

			for _, slot := range slots {
				if slot < n.getEpochFirstSlot(n.phase1TestGenesis) {
					// wait until genesis epoch starts
					n.logger.Debug("skipping slot, lower than genesis", zap.Uint64("genesis_slot", n.getEpochFirstSlot(n.phase1TestGenesis)), zap.Uint64("slot", slot))
					continue
				}
				go func(slot uint64) {
					n.logger.Info("scheduling duty processing start for slot",
						zap.Time("start_time", n.getSlotStartTime(slot)),
						zap.Uint64("committee_index", duty.GetCommitteeIndex()),
						zap.Uint64("slot", slot))

					dutyStruct := slotqueue.Duty{
						NodeID:     validator.NodeID,
						Duty:       duty,
						PublicKey:  validator.PubKey,
						PrivateKey: validator.ShareKey,
						Committee:  validator.Committiee,
					}

					// execute task if slot already began
					if slot == uint64(n.getCurrentSlot()) {
						prevIdentifier := ibft.FirstInstanceIdentifier()
						go n.executeDuty(ctx, prevIdentifier, slot, &dutyStruct)
					} else {
						if err := n.slotQueue.Schedule(validator.PubKey.Serialize(), slot, &dutyStruct); err != nil {
							n.logger.Error("failed to schedule slot")
						}
					}
				}(slot)
			}
		}(duty)
	}

	return nil
}

func (n *ssvNode) listenToNetworkMessages() {
	sigChan := n.network.ReceivedSignatureChan()
	for sigMsg := range sigChan {
		n.queue.AddMessage(&network.Message{
			Lambda:        sigMsg.Message.Lambda,
			SignedMessage: sigMsg,
			Type:          network.NetworkMsg_SignatureType,
		})
	}
}

// startSlotQueueListener starts slot queue listener
func (n *ssvNode) startSlotQueueListener(ctx context.Context, validatorPubKey *bls.PublicKey) {
	n.logger.Info("start listening slot queue")

	prevIdentifier := ibft.FirstInstanceIdentifier()
	for {
		slot, duty, ok, err := n.slotQueue.Next(validatorPubKey.Serialize())
		if err != nil {
			n.logger.Error("failed to get next slot data", zap.Error(err))
			continue
		}

		if !ok {
			n.logger.Debug("no duties for slot scheduled")
			continue
		}
		go n.executeDuty(ctx, prevIdentifier, slot, duty)
	}
}

// getSlotStartTime returns the start time for the given slot
func (n *ssvNode) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// getCurrentSlot returns the current beacon node slot
func (n *ssvNode) getCurrentSlot() int64 {
	genesisTime := int64(n.ethNetwork.MinGenesisTime())
	currentTime := time.Now().Unix()
	return (currentTime - genesisTime) / 12
}

// getEpochFirstSlot returns the beacon node first slot in epoch
func (n *ssvNode) getEpochFirstSlot(epoch uint64) uint64 {
	return epoch * 32
}

// collectSlots collects slots from the given duty
func collectSlots(duty *ethpb.DutiesResponse_Duty) []uint64 {
	var slots []uint64
	slots = append(slots, duty.GetAttesterSlot())
	slots = append(slots, duty.GetProposerSlots()...)
	return slots
}
