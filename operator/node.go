package operator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/api"
	exporterstore "github.com/ssvlabs/ssv/exporter/store"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/fee_recipient"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/message"
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

// dutyTraceDecidedsProvider is the minimal interface used from the duty trace collector
// to serve websocket /query decided lookups.
type dutyTraceDecidedsProvider interface {
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
}

type Node struct {
	logger *zap.Logger

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

	traceCollector dutyTraceDecidedsProvider
}

// New is the constructor of Node
func New(logger *zap.Logger, opts Options, exporterOpts exporter.Options, slotTickerProvider slotticker.Provider, qbftStorage *qbftstorage.ParticipantStores) *Node {
	selfValidatorStore := opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID)

	// Prepare scheduler wiring; in exporter mode we swap to AllShares provider,
	// a prefetching beacon adapter, and a no-op executor.
	var schedulerBeacon duties.BeaconNode = opts.BeaconNode
	validatorProvider := any(selfValidatorStore).(duties.ValidatorProvider)
	dutyExecutor := duties.DutyExecutor(opts.ValidatorController)

	if exporterOpts.Enabled {
		validatorProvider = duties.NewAllSharesProvider(opts.ValidatorStore)
		dutyExecutor = duties.NewNoopExecutor()
		schedulerBeacon = duties.NewPrefetchingBeacon(logger, opts.BeaconNode, opts.NetworkConfig.Beacon, opts.ValidatorStore)
	}

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
			BeaconNode:              schedulerBeacon,
			ExecutionClient:         opts.ExecutionClient,
			BeaconConfig:            opts.NetworkConfig.Beacon,
			ValidatorProvider:       validatorProvider,
			ValidatorController:     opts.ValidatorController,
			DutyExecutor:            dutyExecutor,
			IndicesChg:              opts.ValidatorController.IndicesChangeChan(),
			ValidatorRegistrationCh: opts.ValidatorController.ValidatorRegistrationChan(),
			ValidatorExitCh:         opts.ValidatorController.ValidatorExitChan(),
			DutyStore:               opts.DutyStore,
			SlotTickerProvider:      slotTickerProvider,
			P2PNetwork:              opts.P2PNetwork,
			ExporterMode:            exporterOpts.Enabled,
		}),
		feeRecipientCtrl: feeRecipientCtrl,

		ws:             opts.WS,
		wsAPIPort:      opts.WsAPIPort,
		traceCollector: opts.ValidatorOptions.DutyTraceCollector,
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
	n.logger.Info("starting operator node")

	go func() {
		err := n.startWSServer()
		if err != nil {
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

	n.logger.Info("operator node has been started", fields.OperatorID(n.validatorOptions.OperatorDataStore.GetOperatorID()))

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
		nm.Msg = api.Message{
			Type: api.TypeError,
			Data: []string{fmt.Sprintf("could not parse network message: %v", nm.Err)},
		}
	}
	n.logger.Debug("got incoming export request",
		zap.String("type", string(nm.Msg.Type)))

	h := api.NewHandler(n.logger)

	switch nm.Msg.Type {
	case api.TypeDecided:
		// In exporter v2 (archive) mode we collect decided data via the duty trace collector
		// instead of the legacy qbft storage. When the collector is available, serve queries
		// from it to avoid empty responses while no validators are running locally.
		// The check for `nil` allows backward compatibility when running without exporter v2.
		if n.traceCollector != nil {
			n.handleDecidedFromTraceCollector(nm)
		} else {
			h.HandleParticipantsQuery(n.qbftStorage, nm, n.network.DomainType)
		}
	case api.TypeError:
		h.HandleErrorQuery(nm)
	default:
		h.HandleUnknownQuery(nm)
	}
}

// handleDecidedFromTraceCollector responds to /query requests using duty trace data when available.
func (n *Node) handleDecidedFromTraceCollector(nm *api.NetworkMessage) {
	res := api.Message{Type: nm.Msg.Type, Filter: nm.Msg.Filter}

	pkBytes, err := hex.DecodeString(nm.Msg.Filter.PublicKey)
	if err != nil {
		n.logger.Warn("failed to decode validator public key", zap.Error(err))
		res.Type = api.TypeError
		res.Data = []string{fmt.Sprintf("invalid publicKey %q: %v", nm.Msg.Filter.PublicKey, err)}
		nm.Msg = res
		return
	}

	var pk spectypes.ValidatorPK
	copy(pk[:], pkBytes)

	idx, ok := n.validatorOptions.ValidatorStore.ValidatorIndex(pk)
	if !ok {
		n.logger.Warn("validator not found for public key", zap.String("validator_pubkey", hex.EncodeToString(pk[:])))
		res.Type = api.TypeError
		res.Data = []string{fmt.Sprintf("validator not found for public key %s", nm.Msg.Filter.PublicKey)}
		nm.Msg = res
		return
	}

	role, err := message.BeaconRoleFromString(nm.Msg.Filter.Role)
	if err != nil {
		n.logger.Warn("failed to parse role", zap.Error(err))
		res.Type = api.TypeError
		res.Data = []string{fmt.Sprintf("role doesn't exist: %q", nm.Msg.Filter.Role)}
		nm.Msg = res
		return
	}

	participations := make([]qbftstorage.Participation, 0)
	var hasUnexpectedError bool
	var lastUnexpectedErr error

	for slot := phase0.Slot(nm.Msg.Filter.From); slot <= phase0.Slot(nm.Msg.Filter.To); slot++ {
		var entries []dutytracer.ParticipantsRangeIndexEntry
		if role == spectypes.BNRoleAttester || role == spectypes.BNRoleSyncCommittee {
			entries, err = n.traceCollector.GetCommitteeDecideds(slot, idx, role)
		} else {
			entries, err = n.traceCollector.GetValidatorDecideds(role, slot, []phase0.ValidatorIndex{idx})
		}

		if err != nil {
			var merr *multierror.Error
			if errors.As(err, &merr) {
				merr = filterOutDutyNotFoundErrors(merr)
				if merr != nil && merr.ErrorOrNil() != nil {
					hasUnexpectedError = true
					lastUnexpectedErr = merr
					n.logger.Warn("failed to get decided entries from collector", zap.Error(merr), fields.Slot(slot), fields.ValidatorIndex(idx), fields.BeaconRole(role))
				}
			} else if !isNotFoundError(err) {
				hasUnexpectedError = true
				lastUnexpectedErr = err
				n.logger.Warn("failed to get decided entries from collector", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(idx), fields.BeaconRole(role))
			}
			continue
		}

		for _, e := range entries {
			participations = append(participations, qbftstorage.Participation{
				ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
					Slot:    e.Slot,
					PubKey:  pk,
					Signers: e.Signers,
				},
				Role:   role,
				PubKey: pk,
			})
		}
	}

	if len(participations) == 0 {
		if hasUnexpectedError {
			n.logger.Warn("failed to build participants api data due to collector errors", zap.Error(lastUnexpectedErr), fields.ValidatorIndex(idx))
			res.Type = api.TypeError
			res.Data = []string{fmt.Sprintf("internal error - could not build response: %v", lastUnexpectedErr)}
		} else {
			// Mirror legacy exporter behavior: empty range returns "no messages" as a decided response.
			res.Data = []string{"no messages"}
		}
		nm.Msg = res
		return
	}

	data, err := api.ParticipantsAPIData(n.network.DomainType, participations...)
	if err != nil {
		n.logger.Warn("failed to build participants api data", zap.Error(err))
		res.Type = api.TypeError
		res.Data = []string{fmt.Sprintf("internal error - could not build response: %v", err)}
		nm.Msg = res
		return
	}

	res.Data = data
	nm.Msg = res
}

// isNotFoundError returns true if the error represents an expected "no duty"
// condition, either from the duty tracer or the underlying exporter store.
// It mirrors the semantics in exporter2 helpers to ease future refactoring.
func isNotFoundError(err error) bool {
	return errors.Is(err, dutytracer.ErrNotFound) || errors.Is(err, exporterstore.ErrNotFound)
}

// filterOutDutyNotFoundErrors removes not-found duty errors from a multierror,
// returning nil if nothing remains. It mirrors exporter2.filterOutDutyNotFoundErrors
// to make future WS refactoring simpler.
func filterOutDutyNotFoundErrors(e *multierror.Error) *multierror.Error {
	if e == nil || e.ErrorOrNil() == nil {
		return nil
	}
	var filtered *multierror.Error
	for _, err := range e.Errors {
		if !isNotFoundError(err) {
			filtered = multierror.Append(filtered, err)
		}
	}
	return filtered
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
			fields.OperatorPubKey(operators[i].PublicKey),
		)
	}
}
