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
)

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start() error
}

// Options contains options to create the node
type Options struct {
	ETHNetwork *core.Network
	Beacon     *beacon.Beacon
	Context    context.Context
	Logger     *zap.Logger
	// genesis epoch
	GenesisEpoch uint64 `yaml:"GenesisEpoch" env:"GENESIS_EPOCH" env-description:"Genesis Epoch SSV node will start"`
	// max slots for duty to wait
	//TODO switch to time frame?
	DutyLimit        uint64                      `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	ValidatorOptions validator.ControllerOptions `yaml:"ValidatorOptions"`
}

// ssvNode implements Node interface
type ssvNode struct {
	ethNetwork            core.Network
	slotQueue             slotqueue.Queue
	context               context.Context
	validatorController   validator.IController
	logger                *zap.Logger
	beacon                beacon.Beacon
	genesisEpoch          uint64
	dutyLimit             uint64
	streamDuties          <-chan *ethpb.DutiesResponse_Duty
	pubkeysUpdateChan     chan bool

	//eth1Client eth1.Eth1
	// add eth1Client and listen to its events
}

// New is the constructor of ssvNode
func New(opts Options) Node {
	slotQueue := slotqueue.New(*opts.ETHNetwork)
	opts.ValidatorOptions.SlotQueue = slotQueue
	ssv := &ssvNode{
		context:             opts.Context,
		logger:              opts.Logger,
		genesisEpoch:        opts.GenesisEpoch,
		dutyLimit:           opts.DutyLimit,
		validatorController: validator.NewController(opts.ValidatorOptions),
		ethNetwork:          *opts.ETHNetwork,
		beacon:              *opts.Beacon,
		// TODO do we really need to pass the whole object or just SlotDurationSec
		slotQueue: slotQueue,
	}

	return ssv
}

// Start implements Node interface
func (n *ssvNode) Start() error {
	n.validatorController.StartValidators()
	n.startStreamDuties()
	validatorChan := n.validatorController.NewValidatorChan()
	for {
		select {
		case <- validatorChan:
			n.startStreamDuties()
			continue
		case <-n.pubkeysUpdateChan:
			n.logger.Debug("public keys updated, restart stream duties listener")
			continue
		case duty := <-n.streamDuties:
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
						logger := n.logger.
							With(zap.Uint64("committee_index", duty.GetCommitteeIndex())).
							With(zap.Uint64("slot", slot)).
							With(zap.Time("start_time", n.getSlotStartTime(slot)))
						// execute task if slot already began and not pass 1 epoch
						currentSlot := uint64(n.getCurrentSlot())
						if slot >= currentSlot && slot-currentSlot <= n.dutyLimit {
							pubKey := &bls.PublicKey{}
							if err := pubKey.Deserialize(duty.PublicKey); err != nil {
								n.logger.Error("failed to deserialize pubkey from duty")
							}
							v, ok := n.validatorController.GetValidator(pubKey.SerializeToHexStr())
							if ok {
								logger.Info("starting duty processing start for slot")
								go v.ExecuteDuty(n.context, slot, duty)
							} else {
								logger.Info("could not find validator")
							}
						} else {
							logger.Info("scheduling duty processing start for slot")
							if err := n.slotQueue.Schedule(duty.PublicKey, slot, duty); err != nil {
								n.logger.Error("failed to schedule slot")
							}
						}
					}(slot)
				}
			}(duty)
		}
	}
}

// startStreamDuties start to stream duties from the beacon chain
func (n *ssvNode) startStreamDuties() {
	var err error
	pubKeys := n.validatorController.GetValidatorsPubKeys()
	n.streamDuties, err = n.beacon.StreamDuties(n.context, pubKeys)
	if err != nil {
		n.logger.Error("failed to open duties stream", zap.Error(err))
	}
	if n.pubkeysUpdateChan == nil {
		n.pubkeysUpdateChan = make(chan bool) // first init
	} else {
		n.pubkeysUpdateChan <- true // update stream duty listener in order to fetch newly added pubkeys
	}

	n.logger.Info("start streaming duties")
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

// InformObserver informs observer
//func (n *ssvNode) InformObserver(data interface{}) {
//	if validatorShare, ok := data.(storage.Share); ok {
//		v, exist := n.validatorController.GetValidator(validatorShare.PublicKey.SerializeToHexStr())
//		if exist {
//			n.logger.Info("validator already exist", zap.String("pubkey", validatorShare.PublicKey.SerializeToHexStr()))
//			return
//		}
//		// setup validator
//		validatorOpts := validator.Options{
//			Context:                    n.context,
//			Logger:                     n.logger,
//			Share:                      &validatorShare,
//			Network:                    n.network,
//			Beacon:                     n.beacon,
//			ETHNetwork:                 n.ethNetwork,
//			SlotQueue:                  n.slotQueue,
//			SignatureCollectionTimeout: n.signatureCollectionTimeout,
//		}
//		v = validator.New(validatorOpts, n.ibftStorage)
//		n.validatorController.AddValidator(validatorShare.PublicKey.SerializeToHexStr(), v)
//		// start validator
//		if err := v.Start(); err != nil {
//			n.logger.Error("failed to start validator", zap.Error(err))
//		}
//
//		// update stream duties
//		n.startStreamDuties()
//	}
//}

// GetObserverID get the observer id
//func (n *ssvNode) GetObserverID() string {
//	// TODO return proper id for the observer
//	return "SsvNodeObserver"
//}
