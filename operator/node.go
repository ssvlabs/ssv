package operator

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/api"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorageprotocol "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

// Node represents the behavior of SSV node
type Node interface {
	Start() error
	StartEth1(syncOffset *eth1.SyncOffset) error
}

// Options contains options to create the node
type Options struct {
	ETHNetwork          beaconprotocol.Network
	Beacon              beaconprotocol.Beacon
	Network             network.P2PNetwork
	Context             context.Context
	Logger              *zap.Logger
	Eth1Client          eth1.Client
	DB                  basedb.IDb
	ValidatorController validator.Controller
	DutyExec            duties.DutyExecutor
	// genesis epoch
	GenesisEpoch uint64 `yaml:"GenesisEpoch" env:"GENESIS_EPOCH" env-description:"Genesis Epoch SSV node will start"`
	// max slots for duty to wait
	DutyLimit        uint64                      `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	ValidatorOptions validator.ControllerOptions `yaml:"ValidatorOptions"`

	ForkVersion forksprotocol.ForkVersion

	WS        api.WebSocketServer
	WsAPIPort int
}

// operatorNode implements Node interface
type operatorNode struct {
	ethNetwork     beaconprotocol.Network
	context        context.Context
	validatorsCtrl validator.Controller
	logger         *zap.Logger
	beacon         beaconprotocol.Beacon
	net            network.P2PNetwork
	storage        storage.Storage
	qbftStorage    qbftstorageprotocol.QBFTStore
	eth1Client     eth1.Client
	dutyCtrl       duties.DutyController
	//fork           *forks.Forker

	forkVersion forksprotocol.ForkVersion

	ws        api.WebSocketServer
	wsAPIPort int
}

// New is the constructor of operatorNode
func New(opts Options) Node {
	qbftStorage := qbftstorage.New(opts.DB, opts.Logger, message.RoleTypeAttester.String(), opts.ForkVersion)

	node := &operatorNode{
		context:        opts.Context,
		logger:         opts.Logger.With(zap.String("component", "operatorNode")),
		validatorsCtrl: opts.ValidatorController,
		ethNetwork:     opts.ETHNetwork,
		beacon:         opts.Beacon,
		net:            opts.Network,
		eth1Client:     opts.Eth1Client,
		storage:        storage.NewNodeStorage(opts.DB, opts.Logger),
		qbftStorage:    qbftStorage,

		dutyCtrl: duties.NewDutyController(&duties.ControllerOptions{
			Logger:              opts.Logger,
			Ctx:                 opts.Context,
			BeaconClient:        opts.Beacon,
			EthNetwork:          opts.ETHNetwork,
			ValidatorController: opts.ValidatorController,
			GenesisEpoch:        opts.GenesisEpoch,
			DutyLimit:           opts.DutyLimit,
			Executor:            opts.DutyExec,
			ForkVersion:         opts.ForkVersion,
		}),

		forkVersion: opts.ForkVersion,

		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	if err := node.init(opts); err != nil {
		node.logger.Panic("failed to init", zap.Error(err))
	}

	return node
}

func (n *operatorNode) init(opts Options) error {
	if opts.ValidatorOptions.CleanRegistryData {
		if err := n.storage.CleanRegistryData(); err != nil {
			return errors.Wrap(err, "failed to clean registry data")
		}
	}
	return nil
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start() error {
	n.logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer()
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	n.validatorsCtrl.StartValidators()
	n.validatorsCtrl.StartNetworkHandlers()
	go n.validatorsCtrl.UpdateValidatorMetaDataLoop()
	go n.listenForCurrentSlot()
	n.dutyCtrl.Start()

	return nil
}

// listenForCurrentSlot listens to current slot and trigger relevant components if needed
func (n *operatorNode) listenForCurrentSlot() {
	for slot := range n.dutyCtrl.CurrentSlotChan() {
		n.setFork(slot)
	}
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1(syncOffset *eth1.SyncOffset) error {
	n.logger.Info("starting operator node syncing with eth1")

	handler := n.validatorsCtrl.Eth1EventHandler(false)
	// sync past events
	if err := eth1.SyncEth1Events(n.logger, n.eth1Client, n.storage, syncOffset, handler); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	n.logger.Info("manage to sync contract events")
	shares, err := n.validatorsCtrl.GetAllValidatorShares()
	if err != nil {
		n.logger.Error("failed to get validator shares", zap.Error(err))
	}
	operators, err := n.storage.ListOperators(0, 0)
	if err != nil {
		n.logger.Error("failed to get operators", zap.Error(err))
	}
	n.logger.Info("ETH1 sync history stats",
		zap.Int("validators count", len(shares)),
		zap.Int("operators count", len(operators)),
	)

	// setup validator controller to listen to new events
	go n.validatorsCtrl.ListenToEth1Events(n.eth1Client.EventsFeed())

	// starts the eth1 events subscription
	if err := n.eth1Client.Start(); err != nil {
		return errors.Wrap(err, "failed to start eth1 client")
	}

	return nil
}

// HealthCheck returns a list of issues regards the state of the operator node
func (n *operatorNode) HealthCheck() []string {
	return metrics.ProcessAgents(n.healthAgents())
}

func (n *operatorNode) healthAgents() []metrics.HealthCheckAgent {
	var agents []metrics.HealthCheckAgent
	if agent, ok := n.eth1Client.(metrics.HealthCheckAgent); ok {
		agents = append(agents, agent)
	}
	if agent, ok := n.beacon.(metrics.HealthCheckAgent); ok {
		agents = append(agents, agent)
	}
	return agents
}

// handleQueryRequests waits for incoming messages and
func (n *operatorNode) handleQueryRequests(nm *api.NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
	}
	n.logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))
	switch nm.Msg.Type {
	case api.TypeDecided:
		api.HandleDecidedQuery(n.logger, n.qbftStorage, nm)
	case api.TypeError:
		api.HandleErrorQuery(n.logger, nm)
	default:
		api.HandleUnknownQuery(n.logger, nm)
	}
}

func (n *operatorNode) startWSServer() error {
	if n.ws != nil {
		n.logger.Info("starting WS server")

		n.ws.UseQueryHandler(n.handleQueryRequests)

		if err := n.ws.Start(fmt.Sprintf(":%d", n.wsAPIPort)); err != nil {
			return err
		}
	}

	return nil
}
