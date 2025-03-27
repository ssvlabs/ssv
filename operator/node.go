package operator

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter/api"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/fee_recipient"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	storage2 "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// Options contains options to create the node
type Options struct {
	// NetworkName is the network name of this node
	NetworkName         string `yaml:"Network" env:"NETWORK" env-default:"mainnet" env-description:"Ethereum network to connect to (mainnet, holesky, sepolia, etc.)"`
	CustomDomainType    string `yaml:"CustomDomainType" env:"CUSTOM_DOMAIN_TYPE" env-default:"" env-description:"Override SSV domain type for network isolation. Warning: Please modify only if you are certain of the implications. This would be incremented by 1 after Alan fork (e.g., 0x01020304 → 0x01020305 post-fork)"`
	Network             networkconfig.NetworkConfig
	BeaconNode          beaconprotocol.BeaconNode // TODO: consider renaming to ConsensusClient
	ExecutionClient     executionclient.Provider
	P2PNetwork          network.P2PNetwork
	Context             context.Context
	DB                  basedb.Database
	ValidatorController validator.Controller
	ValidatorStore      storage2.ValidatorStore
	ValidatorOptions    validator.ControllerOptions `yaml:"ValidatorOptions"`
	DutyStore           *dutystore.Store
	WS                  api.WebSocketServer
	WsAPIPort           int
}

type Node struct {
	network          networkconfig.NetworkConfig
	context          context.Context
	validatorsCtrl   validator.Controller
	validatorOptions validator.ControllerOptions
	consensusClient  beaconprotocol.BeaconNode
	executionClient  executionclient.Provider
	net              network.P2PNetwork
	storage          storage.Storage
	qbftStorage      *qbftstorage.ParticipantStores
	dutyScheduler    *duties.Scheduler
	feeRecipientCtrl fee_recipient.RecipientController

	ws        api.WebSocketServer
	wsAPIPort int
}

// New is the constructor of Node
func New(logger *zap.Logger, opts Options, slotTickerProvider slotticker.Provider, qbftStorage *qbftstorage.ParticipantStores) *Node {
	node := &Node{
		context:          opts.Context,
		validatorsCtrl:   opts.ValidatorController,
		validatorOptions: opts.ValidatorOptions,
		network:          opts.Network,
		consensusClient:  opts.BeaconNode,
		executionClient:  opts.ExecutionClient,
		net:              opts.P2PNetwork,
		storage:          opts.ValidatorOptions.RegistryStorage,
		qbftStorage:      qbftStorage,
		dutyScheduler: duties.NewScheduler(&duties.SchedulerOptions{
			Ctx:                 opts.Context,
			BeaconNode:          opts.BeaconNode,
			ExecutionClient:     opts.ExecutionClient,
			Network:             opts.Network,
			ValidatorProvider:   opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID),
			ValidatorController: opts.ValidatorController,
			DutyExecutor:        opts.ValidatorController,
			IndicesChg:          opts.ValidatorController.IndicesChangeChan(),
			ValidatorExitCh:     opts.ValidatorController.ValidatorExitChan(),
			DutyStore:           opts.DutyStore,
			SlotTickerProvider:  slotTickerProvider,
			P2PNetwork:          opts.P2PNetwork,
		}),
		feeRecipientCtrl: fee_recipient.NewController(&fee_recipient.ControllerOptions{
			Ctx:                opts.Context,
			BeaconClient:       opts.BeaconNode,
			Network:            opts.Network,
			ShareStorage:       opts.ValidatorOptions.RegistryStorage.Shares(),
			RecipientStorage:   opts.ValidatorOptions.RegistryStorage,
			OperatorDataStore:  opts.ValidatorOptions.OperatorDataStore,
			SlotTickerProvider: slotTickerProvider,
		}),

		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	return node
}

// Start starts to stream duties and run IBFT instances
func (n *Node) Start(logger *zap.Logger) error {
	logger = logger.Named(logging.NameOperator)

	logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer(logger)
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	// Start the duty scheduler, and a background goroutine to crash the node
	// in case there were any errors.
	if err := n.dutyScheduler.Start(n.context, logger); err != nil {
		return fmt.Errorf("failed to run duty scheduler: %w", err)
	}

	n.validatorsCtrl.StartNetworkHandlers()

	if n.validatorOptions.Exporter {
		// Subscribe to all subnets.
		err := n.net.SubscribeAll(logger)
		if err != nil {
			logger.Error("failed to subscribe to all subnets", zap.Error(err))
		}
	}
	go n.net.UpdateSubnets(logger)
	go n.net.UpdateScoreParams(logger)
	n.validatorsCtrl.StartValidators(n.context)
	go n.reportOperators(logger)

	go n.feeRecipientCtrl.Start(logger)
	go n.validatorsCtrl.HandleMetadataUpdates(n.context)
	go n.validatorsCtrl.ReportValidatorStatuses(n.context)

	go func() {
		if err := n.validatorOptions.DoppelgangerHandler.Start(n.context); err != nil {
			logger.Error("Doppelganger monitoring exited with error", zap.Error(err))
		}
	}()

	if err := n.dutyScheduler.Wait(); err != nil {
		logger.Fatal("duty scheduler exited with error", zap.Error(err))
	}

	return nil
}

// HealthCheck returns a list of issues regards the state of the operator node
func (n *Node) HealthCheck() error {
	// TODO: previously this checked availability of consensus & execution clients.
	// However, currently the node crashes when those clients are down,
	// so this health check is currently a positive no-op.
	return nil
}

// handleQueryRequests waits for incoming messages and
func (n *Node) handleQueryRequests(logger *zap.Logger, nm *api.NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
	}
	logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))
	switch nm.Msg.Type {
	case api.TypeDecided:
		api.HandleParticipantsQuery(logger, n.qbftStorage, nm, n.network.DomainType)
	case api.TypeError:
		api.HandleErrorQuery(logger, nm)
	default:
		api.HandleUnknownQuery(logger, nm)
	}
}

func (n *Node) startWSServer(logger *zap.Logger) error {
	if n.ws != nil {
		logger.Info("starting WS server")

		n.ws.UseQueryHandler(n.handleQueryRequests)

		if err := n.ws.Start(logger, fmt.Sprintf(":%d", n.wsAPIPort)); err != nil {
			return err
		}
	}

	return nil
}

func (n *Node) reportOperators(logger *zap.Logger) {
	operators, err := n.storage.ListOperators(nil, 0, 1000) // TODO more than 1000?
	if err != nil {
		logger.Warn("failed to get all operators for reporting", zap.Error(err))
		return
	}
	logger.Debug("reporting operators", zap.Int("count", len(operators)))
	for i := range operators {
		logger.Debug("report operator public key",
			fields.OperatorID(operators[i].ID),
			fields.PubKey(operators[i].PublicKey))
	}
}
