package operator

import (
	"context"
	"encoding/hex"
	api "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"go.uber.org/zap"
	"time"
)

// Node represents the behavior of SSV node
type Node interface {
	Start() error
	StartEth1(syncOffset *eth1.SyncOffset) error
}

// Options contains options to create the node
type Options struct {
	ETHNetwork          *core.Network
	Beacon              *beacon.Beacon
	Context             context.Context
	Logger              *zap.Logger
	Eth1Client          eth1.Client
	DB                  basedb.IDb
	SlotQueue           slotqueue.Queue
	ValidatorController validator.IController
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
	eth1Client          eth1.Client
	epochDutiesExist    bool // mark when received epoch duties in order to prevent fetching duties each slot tick
}

// New is the constructor of operatorNode
func New(opts Options) Node {
	ssv := &operatorNode{
		context:             opts.Context,
		logger:              opts.Logger.With(zap.String("component", "operatorNode")),
		genesisEpoch:        opts.GenesisEpoch,
		dutyLimit:           opts.DutyLimit,
		validatorController: opts.ValidatorController,
		ethNetwork:          *opts.ETHNetwork,
		beacon:              *opts.Beacon,
		storage:             NewOperatorNodeStorage(opts.DB, opts.Logger),
		// TODO do we really need to pass the whole object or just SlotDurationSec
		slotQueue:        opts.SlotQueue,
		eth1Client:       opts.Eth1Client,
		epochDutiesExist: false,
	}

	return ssv
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start() error {
	n.logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")
	n.validatorController.StartValidators()
	return n.startDutiesTicker()
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1(syncOffset *eth1.SyncOffset) error {
	n.logger.Info("starting operator node syncing with eth1")

	// setup validator controller to listen to ValidatorAdded events
	// this will handle events from the sync as well
	cnValidators, err := n.eth1Client.EventsSubject().Register("ValidatorControllerObserver")
	if err != nil {
		return errors.Wrap(err, "failed to register on contract events subject")
	}
	go n.validatorController.ListenToEth1Events(cnValidators)

	// sync past events
	if err := eth1.SyncEth1Events(n.logger, n.eth1Client, n.storage, "SSVNodeEth1Sync", syncOffset); err != nil {
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

func (n *operatorNode) onDuty(duty *beacon.Duty) {
	if uint64(duty.Slot) < n.getEpochFirstSlot(n.genesisEpoch) {
		// wait until genesis epoch starts
		n.logger.Debug("skipping slot, lower than genesis",
			zap.Uint64("genesis_slot", n.getEpochFirstSlot(n.genesisEpoch)),
			zap.Uint64("slot", uint64(duty.Slot)))
		return
	}

	currentSlot := uint64(n.getCurrentSlot())
	logger := n.logger.
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(zap.Uint64("current slot", currentSlot)).
		With(zap.Uint64("slot", uint64(duty.Slot))).
		With(zap.Uint64("epoch", uint64(duty.Slot)/32)).
		With(zap.String("pubKey", hex.EncodeToString(duty.PubKey[:]))).
		With(zap.Time("start_time", n.getSlotStartTime(uint64(duty.Slot))))
	// execute task if slot already began and not pass 1 epoch
	if currentSlot >= uint64(duty.Slot) && currentSlot-uint64(duty.Slot) <= n.dutyLimit {
		pubKey := &bls.PublicKey{}
		if err := pubKey.Deserialize(duty.PubKey[:]); err != nil {
			logger.Error("failed to deserialize pubkey from duty")
		}
		v, ok := n.validatorController.GetValidator(pubKey.SerializeToHexStr())
		if ok {
			logger.Info("starting duty processing start for slot")
			go v.ExecuteDuty(n.context, uint64(duty.Slot), duty)
		} else {
			logger.Info("could not find validator")
		}
	} else {
		logger.Info("scheduling duty processing start for slot")
		if err := n.slotQueue.Schedule(duty.PubKey[:], uint64(duty.Slot), duty); err != nil {
			logger.Error("failed to schedule slot")
		}
	}
}

// startDutiesTicker start to stream duties from the beacon chain
func (n *operatorNode) startDutiesTicker() error {
	genesisTime := time.Unix(int64(n.ethNetwork.MinGenesisTime()), 0)
	slotTicker := slotutil.GetSlotTicker(genesisTime, uint64(n.ethNetwork.SlotDurationSec().Seconds()))
	for currentSlot := range slotTicker.C() {
		n.logger.Debug("slot ticker", zap.Uint64("slot", currentSlot))
		if currentSlot%n.ethNetwork.SlotsPerEpoch() != 0 && n.epochDutiesExist {
			//	Do nothing if not epoch start AND assignments already exist.
			continue
		}
		n.epochDutiesExist = false

		indices := n.validatorController.GetValidatorsIndices() // fetching all validator with index (will return only active once too)
		n.logger.Debug("got indices for get duties", zap.Int("indices count", len(indices)))
		esEpoch := n.ethNetwork.EstimatedEpochAtSlot(currentSlot)
		epoch := spec.Epoch(esEpoch)
		go n.updatedAttesterDuties(epoch, indices) // when scale need to support batches
		//	 TODO add getProposerDuties here
	}
	return errors.New("ticker failed")
}

// updatedAttesterDuties with the following steps -
// 1, request attest duties from beacon node for all active accounts
// 2, schedule in slotQueue each duty to start time
// 3, subscribe all duties committee to subnet
func (n *operatorNode) updatedAttesterDuties(epoch spec.Epoch, indices []spec.ValidatorIndex) {
	if indices == nil {
		return
	}
	n.logger.Debug("fetching attest duties...", zap.Any("epoch", epoch), zap.Any("count", len(indices)))
	resp, err := n.beacon.GetDuties(epoch, indices)
	n.logger.Debug("duties received")
	if err != nil {
		n.logger.Error("failed to get attest duties", zap.Error(err))
		return
	}
	if len(resp) == 0 {
		n.logger.Debug("no duties...")
		return
	}

	var subscriptions []*api.BeaconCommitteeSubscription
	for _, duty := range resp {
		go n.onDuty(&beacon.Duty{
			Type:                    beacon.RoleTypeAttester,
			PubKey:                  duty.PubKey,
			Slot:                    duty.Slot,
			ValidatorIndex:          duty.ValidatorIndex,
			CommitteeIndex:          duty.CommitteeIndex,
			CommitteeLength:         duty.CommitteeLength,
			CommitteesAtSlot:        duty.CommitteesAtSlot,
			ValidatorCommitteeIndex: duty.ValidatorCommitteeIndex,
		})

		subscriptions = append(subscriptions, &api.BeaconCommitteeSubscription{
			ValidatorIndex:   duty.ValidatorIndex,
			Slot:             duty.Slot,
			CommitteeIndex:   duty.CommitteeIndex,
			CommitteesAtSlot: duty.CommitteesAtSlot,
			IsAggregator:     false, // TODO need to handle agg case
		})
	}

	if err := n.beacon.SubscribeToCommitteeSubnet(subscriptions); err != nil {
		n.logger.Warn("failed to subscribe committee to subnet", zap.Error(err))
	//	 TODO should add return? if so could end up inserting redundant duties
	}
	n.epochDutiesExist = true
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
