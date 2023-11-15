package operator

import (
	"context"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/exporter/api"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/duties"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/fee_recipient"
	"github.com/bloxapp/ssv/operator/slotticker"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/storage/basedb"
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

	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRoleValidatorRegistration,
	}
	for _, role := range roles {
		storageMap.Add(role, qbftstorage.New(opts.DB, role.String()))
	}

	node := &operatorNode{
		context:         opts.Context,
		validatorsCtrl:  opts.ValidatorController,
		network:         opts.Network,
		consensusClient: opts.BeaconNode,
		executionClient: opts.ExecutionClient,
		net:             opts.P2PNetwork,
		storage:         opts.ValidatorOptions.RegistryStorage,
		qbftStorage:     storageMap,
		dutyScheduler: duties.NewScheduler(&duties.SchedulerOptions{
			Ctx:                 opts.Context,
			BeaconNode:          opts.BeaconNode,
			Network:             opts.Network,
			ValidatorController: opts.ValidatorController,
			IndicesChg:          opts.ValidatorController.IndicesChangeChan(),
			ExecuteDuty:         opts.ValidatorController.ExecuteDuty,
			BuilderProposals:    opts.ValidatorOptions.BuilderProposals,
			DutyStore:           opts.DutyStore,
			SlotTickerProvider:  slotTickerProvider,
		}),
		feeRecipientCtrl: fee_recipient.NewController(&fee_recipient.ControllerOptions{
			Ctx:                opts.Context,
			BeaconClient:       opts.BeaconNode,
			Network:            opts.Network,
			ShareStorage:       opts.ValidatorOptions.RegistryStorage.Shares(),
			RecipientStorage:   opts.ValidatorOptions.RegistryStorage,
			OperatorData:       opts.ValidatorOptions.OperatorData,
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
	n.validatorsCtrl.StartValidators()
	go n.net.UpdateSubnets(logger)
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
