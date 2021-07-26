package operator

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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
	context             context.Context
	validatorController validator.IController
	logger              *zap.Logger
	beacon              beacon.Beacon
	storage             Storage
	eth1Client          eth1.Client
	//dutyFetcher         duties.DutyFetcher
	dutyDispatcher duties.DutyDispatcher
}

// New is the constructor of operatorNode
func New(opts Options) Node {
	ssv := &operatorNode{
		context:             opts.Context,
		logger:              opts.Logger,
		validatorController: opts.ValidatorController,
		ethNetwork:          *opts.ETHNetwork,
		beacon:              *opts.Beacon,
		storage:             NewOperatorNodeStorage(opts.DB, opts.Logger),
		// TODO do we really need to pass the whole object or just SlotDurationSec
		eth1Client: opts.Eth1Client,
	}
	df := duties.NewDutyFetcher(opts.Logger, *opts.Beacon, opts.ValidatorController, *opts.ETHNetwork)
	ssv.dutyDispatcher = duties.NewDutyDispatcher(opts.Logger, *opts.ETHNetwork, ssv,
		df, opts.GenesisEpoch, opts.DutyLimit)

	return ssv
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start() error {
	n.logger.Info("starting node -> IBFT")
	n.validatorController.StartValidators()
	go n.beacon.StartReceivingBlocks() // in order to get the latest slot (for attestation purposes)
	n.startDutyDispatcher()
	return nil
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1(syncOffset *eth1.SyncOffset) error {
	n.logger.Info("starting node -> eth1")

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

// ExecuteDuty tries to execute the given duty
func (n *operatorNode) ExecuteDuty(duty *beacon.Duty) error {
	logger := n.dutyDispatcher.LoggerWithDutyContext(n.logger, duty)
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(duty.PubKey[:]); err != nil {
		return errors.Wrap(err, "failed to deserialize pubkey from duty")
	}
	if v, ok := n.validatorController.GetValidator(pubKey.SerializeToHexStr()); ok {
		logger.Info("starting duty processing start for slot")
		go v.ExecuteDuty(n.context, uint64(duty.Slot), duty)
	} else {
		logger.Warn("could not find validator")
	}
	return nil
}

// startDutyDispatcher start to stream duties from the beacon chain
func (n *operatorNode) startDutyDispatcher() {
	indices := n.validatorController.GetValidatorsIndices()
	n.logger.Debug("warming up indices, updating internal map (go-client)", zap.Int("indices count", len(indices)))

	n.dutyDispatcher.ListenToTicker()
}
