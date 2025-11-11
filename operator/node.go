package operator

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/api"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
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
	NetworkConfig       *networkconfig.Network
	BeaconNode          beaconprotocol.BeaconNode // TODO: consider renaming to ConsensusClient
	ExecutionClient     executionclient.Provider
	P2PNetwork          network.P2PNetwork
	Context             context.Context
	DB                  basedb.Database
	ValidatorController *validator.Controller
	ValidatorStore      storage2.ValidatorStore
	ValidatorOptions    validator.ControllerOptions `yaml:"ValidatorOptions"`
	DutyStore           *dutystore.Store
	WS                  api.WebSocketServer
	WsAPIPort           int
}

type Node struct {
	logger           *zap.Logger
	network          *networkconfig.Network
	validatorsCtrl   *validator.Controller
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
	selfValidatorStore := opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID)

	feeRecipientCtrl := fee_recipient.NewController(logger, &fee_recipient.ControllerOptions{
		Ctx:                opts.Context,
		BeaconClient:       opts.BeaconNode,
		BeaconConfig:       opts.NetworkConfig.Beacon,
		ValidatorProvider:  selfValidatorStore,
		OperatorDataStore:  opts.ValidatorOptions.OperatorDataStore,
		SlotTickerProvider: slotTickerProvider,
	})

	node := &Node{
		logger:           logger.Named(log.NameOperator),
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
			Ctx:                     opts.Context,
			BeaconNode:              opts.BeaconNode,
			ExecutionClient:         opts.ExecutionClient,
			BeaconConfig:            opts.NetworkConfig.Beacon,
			ValidatorProvider:       selfValidatorStore,
			ValidatorController:     opts.ValidatorController,
			DutyExecutor:            opts.ValidatorController,
			IndicesChg:              opts.ValidatorController.IndicesChangeChan(),
			ValidatorRegistrationCh: opts.ValidatorController.ValidatorRegistrationChan(),
			ValidatorExitCh:         opts.ValidatorController.ValidatorExitChan(),
			DutyStore:               opts.DutyStore,
			SlotTickerProvider:      slotTickerProvider,
			P2PNetwork:              opts.P2PNetwork,
		}),
		feeRecipientCtrl: feeRecipientCtrl,

		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	// Wire the beacon client to the fee recipient controller
	// This allows the beacon client to pull proposal preparations on reconnect
	opts.BeaconNode.SetProposalPreparationsProvider(feeRecipientCtrl.GetProposalPreparations)

	// Subscribe fee recipient controller to validator controller's change notifications
	feeRecipientCtrl.SubscribeToFeeRecipientChanges(opts.ValidatorController.FeeRecipientChangeChan())

	return node
}

// Start starts to stream duties and run IBFT instances
func (n *Node) Start(ctx context.Context) error {
	n.logger.Info("shota is starting SSV operator node...")
	fmt.Print("shota is starting SSV operator node...")

	n.logger.Info("all required services are ready. OPERATOR SUCCESSFULLY CONFIGURED AND NOW RUNNING!")

	go func() {
		err := n.startWSServer()
		if err != nil {
			// TODO: think if we need to panic
			return
		}
	}()

	// Start the duty scheduler, and a background goroutine to crash the node
	// in case there were any errors.
	if err := n.dutyScheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to run duty scheduler: %w", err)
	}

	n.validatorsCtrl.StartNetworkHandlers()

	// IMPORTANT: We must initialize validators regardless of whether we are running exporter or
	// a regular SSV node.
	validatorsInitialized, err := n.validatorsCtrl.InitValidators()
	if err != nil {
		return fmt.Errorf("init validators: %w", err)
	}

	// For regular SSV node, starting a validator will also connect us to subnets that correspond
	// to that validator. But if we don't have validators to start (if none were initialized) -
	// have to subscribe to at least 1 random subnet explicitly to just be able to participate
	// in the network.
	startValidators := func() error {
		if len(validatorsInitialized) == 0 {
			if err := n.net.SubscribeRandoms(1); err != nil {
				return fmt.Errorf("subscribe to 1 random subnet: %w", err)
			}

			n.logger.Info("no validators to start, successfully subscribed to random subnet")

			return nil
		}

		err = n.validatorsCtrl.StartValidators(ctx, validatorsInitialized)
		if err != nil {
			return fmt.Errorf("start validators: %w", err)
		}

		return nil
	}
	if n.exporterOptions.Enabled {
		// For exporter, we want to connect to all subnets.
		startValidators = func() error {
			err := n.net.SubscribeAll()
			if err != nil {
				n.logger.Error("failed to subscribe to all subnets", zap.Error(err))
				return nil
			}
			return nil
		}
	}
	if err = startValidators(); err != nil {
		return err
	}

	go n.net.UpdateSubnets()
	go n.net.UpdateScoreParams()

	go n.reportOperators()

	go n.feeRecipientCtrl.Start(ctx)

	go n.validatorsCtrl.HandleMetadataUpdates(ctx)
	go n.validatorsCtrl.ReportValidatorStatuses(ctx)

	go func() {
		if err := n.validatorOptions.DoppelgangerHandler.Start(ctx); err != nil {
			n.logger.Error("Doppelganger monitoring exited with error", zap.Error(err))
		}
	}()

	if err := n.dutyScheduler.Wait(); err != nil {
		n.logger.Fatal("duty scheduler exited with error", zap.Error(err))
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
	operators, err := n.storage.ListOperatorsAll(nil)
	if err != nil {
		n.logger.Warn("(reporting) couldn't fetch all operators from DB", zap.Error(err))
		return
	}
	n.logger.Debug("(reporting) fetched all stored operators from DB", zap.Int("count", len(operators)))
	for i := range operators {
		n.logger.Debug("(reporting) operator fetched from DB",
			fields.OperatorID(operators[i].ID),
			fields.OperatorPubKey(operators[i].PublicKey))
	}
}
