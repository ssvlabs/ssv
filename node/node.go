package node

import (
	"context"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/slotqueue"
)

// Options contains options to create the node
type Options struct {
	ValidatorStorage           collections.ValidatorStorage
	IbftStorage                collections.IbftStorage
	ETHNetwork                 core.Network
	Network                    network.Network
	Consensus                  string
	Beacon                     beacon.Beacon
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration
	Phase1TestGenesis          uint64
}

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start(ctx context.Context) error
}

// ssvNode implements Node interface
type ssvNode struct {
	validatorStorage collections.ValidatorStorage
	ibftStorage      collections.IbftStorage
	ethNetwork       core.Network
	network          network.Network
	consensus        string
	slotQueue        slotqueue.Queue
	beacon           beacon.Beacon
	logger           *zap.Logger

	// timeouts
	signatureCollectionTimeout time.Duration
	// genesis epoch
	phase1TestGenesis uint64
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	return &ssvNode{
		validatorStorage:           opts.ValidatorStorage,
		ibftStorage:                opts.IbftStorage,
		ethNetwork:                 opts.ETHNetwork,
		network:                    opts.Network,
		consensus:                  opts.Consensus,
		slotQueue:                  slotqueue.New(opts.ETHNetwork),
		beacon:                     opts.Beacon,
		logger:                     opts.Logger,
		signatureCollectionTimeout: opts.SignatureCollectionTimeout,
		// genesis epoch
		phase1TestGenesis: opts.Phase1TestGenesis,
	}
}

// Start implements Node interface
func (n *ssvNode) Start(ctx context.Context) error {
	validatorsShare, err := n.validatorStorage.GetAllValidatorsShare()
	if err != nil {
		n.logger.Fatal("Failed to get validatorStorage share", zap.Error(err))
	}
	validatorsMap := n.setupValidators(ctx, validatorsShare)
	n.startValidators(validatorsMap)

	var pubkeys [][]byte
	for _, val := range validatorsMap {
		pubkeys = append(pubkeys, val.ValidatorShare.PubKey.Serialize())
	}
	streamDuties, err := n.beacon.StreamDuties(ctx, pubkeys)
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

					// execute task if slot already began and not pass 1 epoch
					currentSlot := uint64(n.getCurrentSlot())
					if slot >= currentSlot && slot - currentSlot <= 12 {
						prevIdentifier := ibft.FirstInstanceIdentifier()
						pubKey := &bls.PublicKey{}
						if err := pubKey.Deserialize(duty.PublicKey); err != nil {
							n.logger.Error("Failed to deserialize pubkey from duty")
						}
						v := validatorsMap[pubKey.SerializeToHexStr()]
						go v.ExecuteDuty(ctx, prevIdentifier, slot, duty)
					} else {
						if err := n.slotQueue.Schedule(duty.PublicKey, slot, duty); err != nil {
							n.logger.Error("failed to schedule slot")
						}
					}
				}(slot)
			}
		}(duty)
	}

	return nil
}

// setupValidators for each validatorShare with proper ibft wrappers
func (n *ssvNode) setupValidators(ctx context.Context, validatorsShare []*collections.Validator) map[string]*validator.Validator {
	res := make(map[string]*validator.Validator)
	for _, validatorShare := range validatorsShare {
		res[validatorShare.PubKey.SerializeToHexStr()] = validator.New(ctx, n.logger, validatorShare, n.ibftStorage, n.network, n.ethNetwork, n.beacon, validator.Options{
			SlotQueue:                  n.slotQueue,
			SignatureCollectionTimeout: n.signatureCollectionTimeout,
		})
	}
	n.logger.Info("setup validators done successfully", zap.Int("count", len(res)))
	return res
}

// startValidators functions (queue streaming, msgQueue listen, etc)
func (n *ssvNode) startValidators(validators map[string]*validator.Validator) {
	for _, v := range validators {
		if err := v.Start(); err != nil{
			n.logger.Error("failed to start validator", zap.Error(err))
			continue
		}
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
