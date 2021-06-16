package operator

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"time"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"
)

// Node represents the behavior of SSV node
type Node interface {
	Start() error
	StartEth1() error
}

// Options contains options to create the node
type Options struct {
	ETHNetwork *core.Network
	Beacon     *beacon.Beacon
	Context    context.Context
	Logger     *zap.Logger
	Eth1Client eth1.Client
	DB         basedb.IDb

	// genesis epoch
	GenesisEpoch uint64 `yaml:"GenesisEpoch" env:"GENESIS_EPOCH" env-description:"Genesis Epoch SSV node will start"`
	// max slots for duty to wait
	//TODO switch to time frame?
	DutyLimit        uint64                      `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	ValidatorOptions validator.ControllerOptions `yaml:"ValidatorOptions"`
}

// operatorNode implements Node interface
type operatorNode struct {
	ethNetwork          core.Network
	slotQueue           slotqueue.Queue
	context             context.Context
	validatorController validator.IController
	logger              *zap.Logger
	beacon              beacon.Beacon
	storage             Storage
	genesisEpoch        uint64
	dutyLimit           uint64
	streamDuties        <-chan *ethpb.DutiesResponse_Duty
	eth1Client          eth1.Client
}

// New is the constructor of operatorNode
func New(opts Options) Node {
	slotQueue := slotqueue.New(*opts.ETHNetwork)
	opts.ValidatorOptions.SlotQueue = slotQueue
	ssv := &operatorNode{
		context:             opts.Context,
		logger:              opts.Logger,
		genesisEpoch:        opts.GenesisEpoch,
		dutyLimit:           opts.DutyLimit,
		validatorController: validator.NewController(opts.ValidatorOptions),
		ethNetwork:          *opts.ETHNetwork,
		beacon:              *opts.Beacon,
		storage:             NewOperatorNodeStorage(opts.DB, opts.Logger),
		// TODO do we really need to pass the whole object or just SlotDurationSec
		slotQueue:  slotQueue,
		eth1Client: opts.Eth1Client,
	}

	return ssv
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start() error {
	n.logger.Info("starting node -> IBFT")

	n.validatorController.StartValidators()
	n.startStreamDuties()

	validatorsSubject := n.validatorController.NewValidatorSubject()
	cnValidators, err := validatorsSubject.Register("SsvNodeObserver")
	if err != nil {
		n.logger.Warn("failed to register on validators events subject", zap.Error(err))
		return err
	}

	for {
		select {
		case <-cnValidators:
			n.logger.Debug("new processed validator, restarting stream duties")
			n.startStreamDuties()
			continue
		case duty := <-n.streamDuties:
			go n.onDuty(duty)
		}
	}
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1() error {
	n.logger.Info("starting node -> eth1")

	// setup validator controller to listen to ValidatorAdded events
	// this will handle events from the sync as well
	cnValidators, err := n.eth1Client.EventsSubject().Register("ValidatorControllerObserver")
	if err != nil {
		return errors.Wrap(err, "failed to register on contract events subject")
	}
	go n.validatorController.ListenToEth1Events(cnValidators)

	// sync past events
	if err := eth1.SyncEth1Events(n.logger, n.eth1Client, n.storage, "SSVNodeEth1Sync"); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	n.logger.Info("manage to sync contract events")

	// starts the eth1 events subscription
	err = n.eth1Client.Start()
	if err != nil {
		return errors.Wrap(err, "failed to start eth1 client")
	}

	return nil
}

func (n *operatorNode) onDuty(duty *ethpb.DutiesResponse_Duty) {
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
			currentSlot := uint64(n.getCurrentSlot())
			logger := n.logger.
				With(zap.Uint64("committee_index", duty.GetCommitteeIndex())).
				With(zap.Uint64("current slot", currentSlot)).
				With(zap.Uint64("slot", slot)).
				With(zap.Time("start_time", n.getSlotStartTime(slot)))
			// execute task if slot already began and not pass 1 epoch
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
}

// startStreamDuties start to stream duties from the beacon chain
func (n *operatorNode) startStreamDuties() {
	var err error
	pubKeys := n.validatorController.GetValidatorsPubKeys()
	n.logger.Debug("got pubkeys for stream duties", zap.Int("pubkeys count", len(pubKeys)))
	n.streamDuties, err = n.beacon.StreamDuties(n.context, pubKeys)
	n.logger.Debug("got stream duties")
	if err != nil {
		n.logger.Error("failed to open duties stream", zap.Error(err))
	}
	n.logger.Info("start streaming duties")
}

// getSlotStartTime returns the start time for the given slot
func (n *operatorNode) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(n.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(n.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// getCurrentSlot returns the current beacon node slot
func (n *operatorNode) getCurrentSlot() int64 {
	genesisTime := int64(n.ethNetwork.MinGenesisTime())
	currentTime := time.Now().Unix()
	return (currentTime - genesisTime) / 12
}

// getEpochFirstSlot returns the beacon node first slot in epoch
func (n *operatorNode) getEpochFirstSlot(epoch uint64) uint64 {
	return epoch * 32
}

// collectSlots collects slots from the given duty
func collectSlots(duty *ethpb.DutiesResponse_Duty) []uint64 {
	var slots []uint64
	slots = append(slots, duty.GetAttesterSlot())
	slots = append(slots, duty.GetProposerSlots()...)
	return slots
}
