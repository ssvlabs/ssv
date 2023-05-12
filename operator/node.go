package operator

import (
	"context"
	"fmt"

	"github.com/bloxapp/ssv/logging"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/exporter/api"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/operator/fee_recipient"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
)

// Node represents the behavior of SSV node
type Node interface {
	Start(logger *zap.Logger) error
	StartEth1(logger *zap.Logger, syncOffset *eth1.SyncOffset) error
}

// Options contains options to create the node
type Options struct {
	ETHNetwork          beaconprotocol.Network
	Beacon              beaconprotocol.Beacon
	Eth1Client          eth1.Client
	Network             network.P2PNetwork
	Context             context.Context
	DB                  basedb.IDb
	ValidatorController validator.Controller
	DutyExec            duties.DutyExecutor
	// genesis epoch
	GenesisEpoch uint64 `yaml:"GenesisEpoch" env:"GENESIS_EPOCH" env-default:"156113" env-description:"Genesis Epoch SSV node will start"`
	// max slots for duty to wait
	DutyLimit        uint64                      `yaml:"DutyLimit" env:"DUTY_LIMIT" env-default:"32" env-description:"max slots to wait for duty to start"`
	ValidatorOptions validator.ControllerOptions `yaml:"ValidatorOptions"`

	ForkVersion forksprotocol.ForkVersion

	WS        api.WebSocketServer
	WsAPIPort int
}

// operatorNode implements Node interface
type operatorNode struct {
	ethNetwork       beaconprotocol.Network
	context          context.Context
	ticker           slot_ticker.Ticker
	validatorsCtrl   validator.Controller
	beacon           beaconprotocol.Beacon
	net              network.P2PNetwork
	storage          storage.Storage
	qbftStorage      *qbftstorage.QBFTStores
	eth1Client       eth1.Client
	dutyCtrl         duties.DutyController
	feeRecipientCtrl fee_recipient.RecipientController
	// fork           *forks.Forker

	forkVersion forksprotocol.ForkVersion

	ws        api.WebSocketServer
	wsAPIPort int
}

// New is the constructor of operatorNode
func New(logger *zap.Logger, opts Options) Node {
	storageMap := qbftstorage.NewStores()

	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
	}
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(opts.DB, role.String(), opts.ForkVersion))
	}

	ticker := slot_ticker.NewTicker(opts.Context, opts.ETHNetwork, phase0.Epoch(opts.GenesisEpoch))

	node := &operatorNode{
		context:        opts.Context,
		ticker:         ticker,
		validatorsCtrl: opts.ValidatorController,
		ethNetwork:     opts.ETHNetwork,
		beacon:         opts.Beacon,
		net:            opts.Network,
		eth1Client:     opts.Eth1Client,
		storage:        opts.ValidatorOptions.RegistryStorage,
		qbftStorage:    storageMap,
		dutyCtrl: duties.NewDutyController(logger, &duties.ControllerOptions{
			Ctx:                 opts.Context,
			BeaconClient:        opts.Beacon,
			EthNetwork:          opts.ETHNetwork,
			ValidatorController: opts.ValidatorController,
			DutyLimit:           opts.DutyLimit,
			Executor:            opts.DutyExec,
			ForkVersion:         opts.ForkVersion,
			Ticker:              ticker,
		}),
		feeRecipientCtrl: fee_recipient.NewController(&fee_recipient.ControllerOptions{
			Ctx:              opts.Context,
			BeaconClient:     opts.Beacon,
			EthNetwork:       opts.ETHNetwork,
			ShareStorage:     opts.ValidatorOptions.RegistryStorage,
			RecipientStorage: opts.ValidatorOptions.RegistryStorage,
			Ticker:           ticker,
			OperatorData:     opts.ValidatorOptions.OperatorData,
		}),
		forkVersion: opts.ForkVersion,

		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	if err := node.init(opts); err != nil {
		logger.Panic("failed to init", zap.Error(err))
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
func (n *operatorNode) Start(logger *zap.Logger) error {
	logger.Named(logging.NameOperator)

	logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer(logger)
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	// slot ticker init
	go n.ticker.Start(logger)

	n.validatorsCtrl.StartNetworkHandlers(logger)
	n.validatorsCtrl.StartValidators(logger)
	go n.net.UpdateSubnets(logger)
	go n.validatorsCtrl.UpdateValidatorMetaDataLoop(logger)
	go n.listenForCurrentSlot(logger)
	go n.reportOperators(logger)

	go n.feeRecipientCtrl.Start(logger)
	n.dutyCtrl.Start(logger)

	return nil
}

// listenForCurrentSlot listens to current slot and trigger relevant components if needed
func (n *operatorNode) listenForCurrentSlot(logger *zap.Logger) {
	tickerChan := make(chan phase0.Slot, 32)
	n.ticker.Subscribe(tickerChan)
	for slot := range tickerChan {
		n.setFork(logger, slot)
	}
}

// StartEth1 starts the eth1 events sync and streaming
func (n *operatorNode) StartEth1(logger *zap.Logger, syncOffset *eth1.SyncOffset) error {
	logger.Info("starting operator node syncing with eth1")

	handler := n.validatorsCtrl.Eth1EventHandler(logger, false)
	// sync past events
	if err := eth1.SyncEth1Events(logger, n.eth1Client, n.storage, syncOffset, handler); err != nil {
		return errors.Wrap(err, "failed to sync contract events")
	}
	logger.Info("manage to sync contract events")
	shares, err := n.storage.GetAllShares(logger)
	if err != nil {
		logger.Error("failed to get validator shares", zap.Error(err))
	}
	operators, err := n.storage.ListOperators(logger, 0, 0)
	if err != nil {
		logger.Error("failed to get operators", zap.Error(err))
	}
	operatorID := n.validatorsCtrl.GetOperatorData().ID
	operatorValidatorsCount := 0
	if operatorID != 0 {
		for _, share := range shares {
			if share.BelongsToOperator(operatorID) {
				operatorValidatorsCount++
			}
		}
	}

	logger.Info("ETH1 sync history stats",
		zap.Int("validators count", len(shares)),
		zap.Int("operators count", len(operators)),
		zap.Int("my validators count", operatorValidatorsCount),
	)

	// setup validator controller to listen to new events
	go n.validatorsCtrl.ListenToEth1Events(logger, n.eth1Client.EventsFeed())

	// starts the eth1 events subscription
	if err := n.eth1Client.Start(logger); err != nil {
		return errors.Wrap(err, "failed to start eth1 client")
	}

	return nil
}

// HealthCheck returns a list of issues regards the state of the operator node
func (n *operatorNode) HealthCheck() []string {
	errs := metrics.ProcessAgents(n.healthAgents())
	metrics.ReportSSVNodeHealthiness(len(errs) == 0)
	return errs
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
func (n *operatorNode) handleQueryRequests(logger *zap.Logger, nm *api.NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
	}
	logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))
	switch nm.Msg.Type {
	case api.TypeDecided:
		api.HandleDecidedQuery(logger, n.qbftStorage, nm)
	case api.TypeError:
		api.HandleErrorQuery(logger, nm)
	default:
		api.HandleUnknownQuery(logger, nm)
	}
}

func (n *operatorNode) startWSServer(logger *zap.Logger) error {
	if n.ws != nil {
		logger.Info("starting WS server")

		n.ws.UseQueryHandler(n.handleQueryRequests)

		if err := n.ws.Start(logger, fmt.Sprintf(":%d", n.wsAPIPort)); err != nil {
			return err
		}
	}

	return nil
}

func (n *operatorNode) reportOperators(logger *zap.Logger) {
	operators, err := n.storage.ListOperators(logger, 0, 1000) // TODO more than 1000?
	if err != nil {
		logger.Warn("failed to get all operators for reporting", zap.Error(err))
		return
	}
	logger.Debug("reporting operators", zap.Int("count", len(operators)))
	for i := range operators {
		exporter.ReportOperatorIndex(logger, &operators[i])
	}
}
