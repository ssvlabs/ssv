package node

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"time"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft"
)

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start() error
}

// Options contains options to create the node
type Options struct {
	//ValidatorStorage collections.IValidator
	//IbftStorage      collections.Iibft
	//Network          network.Network
	ETHNetwork   *core.Network
	Beacon       *beacon.Beacon
	Context      context.Context
	Logger       *zap.Logger
	GenesisEpoch uint64 `yaml:"GenesisEpoch" env:"GENESIS_EPOCH" env-description:"Genesis Epoch SSV node will start"`
	DutyLimit    uint64 `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	// max slots for duty to wait
	//TODO switch to time frame?
	// genesis epoch
	ValidatorOptions *validator.ControllerOptions `yaml:"ValidatorOptions"`
}

// ssvNode implements Node interface
type ssvNode struct {
	//validatorStorage collections.IValidator
	//ibftStorage      collections.Iibft
	//network          network.Network
	ethNetwork          core.Network
	slotQueue           slotqueue.Queue
	context             context.Context
	validatorController validator.IController
	logger              *zap.Logger
	beacon              beacon.Beacon

	// genesis epoch
	genesisEpoch uint64
	// max slots for duty to wait
	dutyLimit uint64
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	ssv := &ssvNode{
		context:             opts.Context,
		logger:              opts.Logger,
		genesisEpoch:        opts.GenesisEpoch,
		dutyLimit:           opts.DutyLimit,
		validatorController: validator.NewController(*opts.ValidatorOptions),
		ethNetwork:          *opts.ETHNetwork,
		beacon:              *opts.Beacon,
		// TODO do we really need to pass the whole object or just SlotDurationSec
		slotQueue: slotqueue.New(*opts.ETHNetwork),
	}

	return ssv

	//return &ssvNode{
	//	//validatorStorage:           opts.ValidatorStorage,
	//	//ibftStorage:                opts.IbftStorage,
	//	//ethNetwork:                 opts.ETHNetwork,
	//	//network:                    opts.Network,
	//	//slotQueue:                  slotqueue.New(opts.ETHNetwork),
	//	//beacon:                     opts.Beacon,
	//	logger: opts.Logger,
	//	//signatureCollectionTimeout: opts.SignatureCollectionTimeout,
	//	// genesis epoch
	//	genesisEpoch: opts.GenesisEpoch,
	//	DutyLimit:    opts.DutyLimit,
	//}
}

// Start implements Node interface
func (n *ssvNode) Start() error {
	validatorsMap := n.validatorController.StartValidators()

	var pubkeys [][]byte
	for _, val := range validatorsMap {
		pubkeys = append(pubkeys, val.Share.PublicKey.Serialize())
	}
	streamDuties, err := n.beacon.StreamDuties(n.context, pubkeys)
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
				if slot < n.getEpochFirstSlot(n.genesisEpoch) {
					// wait until genesis epoch starts
					n.logger.Debug("skipping slot, lower than genesis", zap.Uint64("genesis_slot", n.getEpochFirstSlot(n.genesisEpoch)), zap.Uint64("slot", slot))
					continue
				}
				go func(slot uint64) {
					// execute task if slot already began and not pass 1 epoch
					currentSlot := uint64(n.getCurrentSlot())
					if slot >= currentSlot && slot-currentSlot <= n.dutyLimit {
						prevIdentifier := ibft.FirstInstanceIdentifier()
						pubKey := &bls.PublicKey{}
						if err := pubKey.Deserialize(duty.PublicKey); err != nil {
							n.logger.Error("Failed to deserialize pubkey from duty")
						}
						v := validatorsMap[pubKey.SerializeToHexStr()]
						n.logger.Info("starting duty processing start for slot",
							zap.Uint64("committee_index", duty.GetCommitteeIndex()),
							zap.Uint64("slot", slot))
						go v.ExecuteDuty(n.context, prevIdentifier, slot, duty)
					} else {
						n.logger.Info("scheduling duty processing start for slot",
							zap.Time("start_time", n.getSlotStartTime(slot)),
							zap.Uint64("committee_index", duty.GetCommitteeIndex()),
							zap.Uint64("slot", slot))

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
