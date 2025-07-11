package operator

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter"
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
	NetworkName         string             `yaml:"Network" env:"NETWORK" env-default:"mainnet" env-description:"Ethereum network to connect to (mainnet, holesky, sepolia, etc.). For backwards compatibility it's ignored if CustomNetwork is set"`
	CustomNetwork       *networkconfig.SSV `yaml:"CustomNetwork" env:"CUSTOM_NETWORK" env-description:"Custom SSV network configuration"`
	CustomDomainType    string             `yaml:"CustomDomainType" env:"CUSTOM_DOMAIN_TYPE" env-default:"" env-description:"Override SSV domain type for network isolation. Warning: Please modify only if you are certain of the implications. This would be incremented by 1 after Alan fork (e.g., 0x01020304 → 0x01020305 post-fork)"` // DEPRECATED: use CustomNetwork instead.
	NetworkConfig       *networkconfig.NetworkConfig
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
	logger           *zap.Logger
	network          *networkconfig.NetworkConfig
	context          context.Context
	validatorsCtrl   validator.Controller
	validatorOptions validator.ControllerOptions
	exporterOptions  exporter.Options
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
func New(logger *zap.Logger, opts Options, exporterOpts exporter.Options, slotTickerProvider slotticker.Provider, qbftStorage *qbftstorage.ParticipantStores) *Node {
	node := &Node{
		logger:           logger.Named(logging.NameOperator),
		context:          opts.Context,
		validatorsCtrl:   opts.ValidatorController,
		validatorOptions: opts.ValidatorOptions,
		exporterOptions:  exporterOpts,
		network:          opts.NetworkConfig,
		consensusClient:  opts.BeaconNode,
		executionClient:  opts.ExecutionClient,
		net:              opts.P2PNetwork,
		storage:          opts.ValidatorOptions.RegistryStorage,
		qbftStorage:      qbftStorage,
		dutyScheduler: duties.NewScheduler(logger, &duties.SchedulerOptions{
			Ctx:                 opts.Context,
			BeaconNode:          opts.BeaconNode,
			ExecutionClient:     opts.ExecutionClient,
			BeaconConfig:        opts.NetworkConfig.Beacon,
			ValidatorProvider:   opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID),
			ValidatorController: opts.ValidatorController,
			DutyExecutor:        opts.ValidatorController,
			IndicesChg:          opts.ValidatorController.IndicesChangeChan(),
			ValidatorExitCh:     opts.ValidatorController.ValidatorExitChan(),
			DutyStore:           opts.DutyStore,
			SlotTickerProvider:  slotTickerProvider,
			P2PNetwork:          opts.P2PNetwork,
		}),
		feeRecipientCtrl: fee_recipient.NewController(logger, &fee_recipient.ControllerOptions{
			Ctx:                opts.Context,
			BeaconClient:       opts.BeaconNode,
			BeaconConfig:       opts.NetworkConfig.Beacon,
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
func (n *Node) Start() error {
	ctx := n.context // TODO: pass it to Start
	n.logger.Info("all required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer()
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	if !n.exporterOptions.Enabled {
		// Start the duty scheduler, and a background goroutine to crash the node
		// in case there were any errors.
		if err := n.dutyScheduler.Start(ctx); err != nil {
			return fmt.Errorf("failed to run duty scheduler: %w", err)
		}
	}

	n.validatorsCtrl.StartNetworkHandlers()

	if n.exporterOptions.Enabled {
		// Subscribe to all subnets.
		err := n.net.SubscribeAll()
		if err != nil {
			n.logger.Error("failed to subscribe to all subnets", zap.Error(err))
		}
	}
	go n.net.UpdateSubnets()
	go n.net.UpdateScoreParams()
	n.validatorsCtrl.StartValidators(ctx)
	go n.reportOperators()

	go n.feeRecipientCtrl.Start(ctx)
	go n.validatorsCtrl.HandleMetadataUpdates(ctx)
	go n.validatorsCtrl.ReportValidatorStatuses(ctx)

	go func() {
		if err := n.validatorOptions.DoppelgangerHandler.Start(ctx); err != nil {
			n.logger.Error("Doppelganger monitoring exited with error", zap.Error(err))
		}
	}()

	if n.exporterOptions.Enabled {
		n.logger.Info("exporter is enabled, duty scheduler will not run")
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		n.logger.Info("received shutdown signal")
	} else {
		if err := n.dutyScheduler.Wait(); err != nil {
			n.logger.Fatal("duty scheduler exited with error", zap.Error(err))
		}
	}

	if err := n.net.Close(); err != nil {
		n.logger.Error("could not close network", zap.Error(err))
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
func (n *Node) handleQueryRequests(nm *api.NetworkMessage) {
	if nm.Err != nil {
		nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
	}
	n.logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))

	h := api.NewHandler(n.logger)

	switch nm.Msg.Type {
	case api.TypeDecided:
		h.HandleParticipantsQuery(n.qbftStorage, nm, n.network.DomainType)
	case api.TypeError:
		h.HandleErrorQuery(nm)
	default:
		h.HandleUnknownQuery(nm)
	}
}

func (n *Node) startWSServer() error {
	if n.ws != nil {
		n.logger.Info("starting WS server")

		n.ws.UseQueryHandler(n.handleQueryRequests)

		if err := n.ws.Start(fmt.Sprintf(":%d", n.wsAPIPort)); err != nil {
			return err
		}
	}

	return nil
}

func (n *Node) reportOperators() {
	operators, err := n.storage.ListOperators(nil, 0, 1000) // TODO more than 1000?
	if err != nil {
		n.logger.Warn("failed to get all operators for reporting", zap.Error(err))
		return
	}
	n.logger.Debug("reporting operators", zap.Int("count", len(operators)))
	for i := range operators {
		n.logger.Debug("report operator public key",
			fields.OperatorID(operators[i].ID),
			fields.OperatorPubKey(operators[i].PublicKey))
	}
}
