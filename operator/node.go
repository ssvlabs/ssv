package operator

import (
	"context"
	"fmt"

	storage2 "github.com/ssvlabs/ssv/registry/storage"

	"github.com/ssvlabs/ssv/network"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter/api"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/fee_recipient"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// Node represents the behavior of SSV node
type Node interface {
	Start(logger *zap.Logger) error
}

// Options contains options to create the node
type Options struct {
	// NetworkName is the network name of this node
	NetworkName         string `yaml:"Network" env:"NETWORK" env-default:"mainnet" env-description:"Network is the network of this node"`
	Network             networkconfig.NetworkConfig
	BeaconNode          beaconprotocol.BeaconNode // TODO: consider renaming to ConsensusClient
	ExecutionClient     *executionclient.ExecutionClient
	P2PNetwork          network.P2PNetwork
	Context             context.Context
	DB                  basedb.Database
	ValidatorController validator.Controller
	ValidatorStore      storage2.ValidatorStore
	ValidatorOptions    validator.ControllerOptions `yaml:"ValidatorOptions"`
	DutyStore           *dutystore.Store
	WS                  api.WebSocketServer
	WsAPIPort           int
	Metrics             nodeMetrics
}

// operatorNode implements Node interface
type operatorNode struct {
	network          networkconfig.NetworkConfig
	context          context.Context
	validatorsCtrl   validator.Controller
	validatorOptions validator.ControllerOptions
	consensusClient  beaconprotocol.BeaconNode
	executionClient  *executionclient.ExecutionClient
	net              network.P2PNetwork
	storage          storage.Storage
	qbftStorage      *qbftstorage.QBFTStores
	dutyScheduler    *duties.Scheduler
	feeRecipientCtrl fee_recipient.RecipientController

	ws        api.WebSocketServer
	wsAPIPort int

	metrics nodeMetrics
}

// New is the constructor of operatorNode
func New(logger *zap.Logger, opts Options, slotTickerProvider slotticker.Provider) Node {
	storageMap := qbftstorage.NewStores()

	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleSyncCommitteeContribution,
		spectypes.RoleValidatorRegistration,
		spectypes.RoleVoluntaryExit,
	}
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(opts.DB, role.String()))
	}

	node := &operatorNode{
		context:          opts.Context,
		validatorsCtrl:   opts.ValidatorController,
		validatorOptions: opts.ValidatorOptions,
		network:          opts.Network,
		consensusClient:  opts.BeaconNode,
		executionClient:  opts.ExecutionClient,
		net:              opts.P2PNetwork,
		storage:          opts.ValidatorOptions.RegistryStorage,
		qbftStorage:      storageMap,
		dutyScheduler: duties.NewScheduler(&duties.SchedulerOptions{
			Ctx:                  opts.Context,
			BeaconNode:           opts.BeaconNode,
			ExecutionClient:      opts.ExecutionClient,
			Network:              opts.Network,
			ValidatorProvider:    opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID),
			IndicesChg:           opts.ValidatorController.IndicesChangeChan(),
			ValidatorExitCh:      opts.ValidatorController.ValidatorExitChan(),
			ExecuteDuty:          opts.ValidatorController.ExecuteDuty,
			ExecuteCommitteeDuty: opts.ValidatorController.ExecuteCommitteeDuty,
			DutyStore:            opts.DutyStore,
			SlotTickerProvider:   slotTickerProvider,
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

		metrics: opts.Metrics,
	}

	if node.metrics == nil {
		node.metrics = nopMetrics{}
	}

	return node
}

// Start starts to stream duties and run IBFT instances
func (n *operatorNode) Start(logger *zap.Logger) error {
	logger.Named(logging.NameOperator)

	logger.Info("All required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer(logger)
		if err != nil {
			println("<<<<<<<<<<<<<<<<<<<<<here>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
			println(err)
			println("<<<<<<<<<<<<<<<<<<<<<here>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
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
	n.validatorsCtrl.StartValidators()
	go n.reportOperators(logger)

	go n.feeRecipientCtrl.Start(logger)
	go n.validatorsCtrl.UpdateValidatorMetaDataLoop()

	if err := n.dutyScheduler.Wait(); err != nil {
		logger.Fatal("duty scheduler exited with error", zap.Error(err))
	}

	return nil
}

// HealthCheck returns a list of issues regards the state of the operator node
func (n *operatorNode) HealthCheck() error {
	// TODO: previously this checked availability of consensus & execution clients.
	// However, currently the node crashes when those clients are down,
	// so this health check is currently a positive no-op.
	return nil
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
	case api.TypeParticipants:
		api.HandleParticipantsQuery(logger, n.qbftStorage, nm)
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
	operators, err := n.storage.ListOperators(nil, 0, 1000) // TODO more than 1000?
	if err != nil {
		logger.Warn("failed to get all operators for reporting", zap.Error(err))
		return
	}
	logger.Debug("reporting operators", zap.Int("count", len(operators)))
	for i := range operators {
		n.metrics.OperatorPublicKey(operators[i].ID, operators[i].PublicKey)
		logger.Debug("report operator public key",
			fields.OperatorID(operators[i].ID),
			fields.PubKey(operators[i].PublicKey))
	}
}
