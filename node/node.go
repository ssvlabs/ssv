package node

import (
	"context"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator"
)

// Options contains options to create the node
type Options struct {
	ValidatorStorage collections.IValidatorStorage
	IbftStorage      collections.Iibft
	ETHNetwork       core.Network
	Network          network.Network
	Consensus        string
	Beacon           beacon.Beacon
	Logger           *zap.Logger
	// timeouts
	SignatureCollectionTimeout time.Duration
	// genesis epoch
	GenesisEpoch uint64
	// max slots for duty to wait
	DutySlotsLimit uint64
	Context        context.Context
}

// Node represents the behavior of SSV node
type Node interface {
	// Start starts the SSV node
	Start() error
}

// SsvNode implements Node interface
type SsvNode struct {
	validatorStorage  collections.IValidatorStorage
	ibftStorage       collections.Iibft
	ethNetwork        core.Network
	network           network.Network
	consensus         string
	slotQueue         slotqueue.Queue
	beacon            beacon.Beacon
	logger            *zap.Logger
	validatorsMap     map[string]*validator.Validator
	streamDuties      <-chan *ethpb.DutiesResponse_Duty
	pubkeysUpdateChan chan bool
	context           context.Context

	// timeouts
	signatureCollectionTimeout time.Duration
	// genesis epoch
	genesisEpoch uint64
	// max slots for duty to wait
	dutySlotsLimit uint64
	pubsub.BaseObserver
}

// New is the constructor of SsvNode
func New(opts Options) *SsvNode {
	return &SsvNode{
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
		genesisEpoch:   opts.GenesisEpoch,
		dutySlotsLimit: opts.DutySlotsLimit,
		validatorsMap:  make(map[string]*validator.Validator),
		context:        opts.Context,
	}
}

// InformObserver informs observer
func (n *SsvNode) InformObserver(data interface{}) {
	if validatorShare, ok := data.(collections.ValidatorShare); ok {
		if _, ok := n.validatorsMap[validatorShare.ValidatorPK.SerializeToHexStr()]; ok {
			n.logger.Info("validator already exist", zap.String("pubkey", validatorShare.ValidatorPK.SerializeToHexStr()))
			return
		}
		// setup validator
		n.validatorsMap[validatorShare.ValidatorPK.SerializeToHexStr()] = validator.New(n.context, n.logger, &validatorShare, n.ibftStorage, n.network, n.ethNetwork, n.beacon, validator.Options{
			SlotQueue:                  n.slotQueue,
			SignatureCollectionTimeout: n.signatureCollectionTimeout,
		})

		// start validator
		if err := n.validatorsMap[validatorShare.ValidatorPK.SerializeToHexStr()].Start(); err != nil {
			n.logger.Error("failed to start validator", zap.Error(err))
		}

		// update stream duties
		n.startStreamDuties()
	}
}

// GetObserverID get the observer id
func (n *SsvNode) GetObserverID() string {
	// TODO return proper id for the observer
	return "SsvNodeObserver"
}

// Start implements Node interface
func (n *SsvNode) Start() error {
	n.setupValidators()
	n.startValidators()
	n.startStreamDuties()

	for {
		select {
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
						if slot >= currentSlot && slot-currentSlot <= n.dutySlotsLimit {
							pubKey := &bls.PublicKey{}
							if err := pubKey.Deserialize(duty.PublicKey); err != nil {
								n.logger.Error("Failed to deserialize pubkey from duty")
							}
							v := n.validatorsMap[pubKey.SerializeToHexStr()]
							logger.Info("starting duty processing start for slot")
							go v.ExecuteDuty(n.context, slot, duty)
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

// setupValidators for each validatorShare with proper ibft wrappers
func (n *SsvNode) setupValidators() {
	validatorShares, err := n.validatorStorage.GetAllValidatorShares()
	if err != nil {
		n.logger.Fatal("Failed to get all validator shares", zap.Error(err))
	}

	res := make(map[string]*validator.Validator)
	for _, validatorShare := range validatorShares {
		res[validatorShare.ValidatorPK.SerializeToHexStr()] = validator.New(n.context, n.logger, validatorShare, n.ibftStorage, n.network, n.ethNetwork, n.beacon, validator.Options{
			SlotQueue:                  n.slotQueue,
			SignatureCollectionTimeout: n.signatureCollectionTimeout,
		})
	}
	n.logger.Info("setup validators done successfully", zap.Int("count", len(res)))
	n.validatorsMap = res
}

// startValidators functions (queue streaming, msgQueue listen, etc)
func (n *SsvNode) startValidators() {
	for _, v := range n.validatorsMap {
		if err := v.Start(); err != nil {
			n.logger.Error("failed to start validator", zap.Error(err))
			continue
		}
	}
}

// startStreamDuties start to stream duties from the beacon chain
func (n *SsvNode) startStreamDuties() {
	var pubKeys [][]byte
	var err error
	for _, val := range n.validatorsMap {
		pubKeys = append(pubKeys, val.ValidatorShare.ValidatorPK.Serialize())
	}
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
func (n *SsvNode) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// getCurrentSlot returns the current beacon node slot
func (n *SsvNode) getCurrentSlot() int64 {
	genesisTime := int64(n.ethNetwork.MinGenesisTime())
	currentTime := time.Now().Unix()
	return (currentTime - genesisTime) / 12
}

// getEpochFirstSlot returns the beacon node first slot in epoch
func (n *SsvNode) getEpochFirstSlot(epoch uint64) uint64 {
	return epoch * 32
}

// collectSlots collects slots from the given duty
func collectSlots(duty *ethpb.DutiesResponse_Duty) []uint64 {
	var slots []uint64
	slots = append(slots, duty.GetAttesterSlot())
	slots = append(slots, duty.GetProposerSlots()...)
	return slots
}
