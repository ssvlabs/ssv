package operator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/eth/executionclient"
	exportercore "github.com/ssvlabs/ssv/exporter"
	exporterconfig "github.com/ssvlabs/ssv/exporter/config"
	dutytracer "github.com/ssvlabs/ssv/exporter/dutytracer"
	exporterstore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter/v1/api"
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
	"github.com/ssvlabs/ssv/protocol/v2/message"
	storage2 "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// Options contains options to create the node
type Options struct {
	NetworkName         string             `yaml:"Network" env:"NETWORK" env-description:"Ethereum network to connect to (mainnet, holesky, sepolia, etc.). For backwards compatibility it's ignored if CustomNetwork is set"`
	CustomNetwork       *networkconfig.SSV `yaml:"CustomNetwork" env:"CUSTOM_NETWORK" env-description:"Custom SSV network configuration"`
	CustomDomainType    string             `yaml:"CustomDomainType" env:"CUSTOM_DOMAIN_TYPE" env-description:"Override SSV domain type for network isolation. Warning: Please modify only if you are certain of the implications. This would be incremented by 1 after Alan fork (e.g., 0x01020304 → 0x01020305 post-fork)"` // DEPRECATED: use CustomNetwork instead.
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
	ExporterRead        *exportercore.Exporter
	WS                  api.WebSocketServer
}

func (o *Options) ApplyDefaults() {
	o.NetworkName = "mainnet"
	o.ValidatorOptions.ApplyDefaults()
}

type Node struct {
	logger *zap.Logger

	network          *networkconfig.Network
	validatorsCtrl   *validator.Controller
	validatorOptions validator.ControllerOptions
	exporterOptions  exporterconfig.Options
	consensusClient  beaconprotocol.BeaconNode
	executionClient  executionclient.Provider
	net              network.P2PNetwork
	storage          storage.Storage
	qbftStorage      *qbftstorage.ParticipantStores
	dutyScheduler    *duties.Scheduler
	feeRecipientCtrl fee_recipient.RecipientController

	ws api.WebSocketServer

	exporterRead *exportercore.Exporter
}

func shouldRunDutyScheduler(exporterOpts exporterconfig.Options) bool {
	return !exporterOpts.Enabled || exporterOpts.Mode == exporterconfig.ModeArchive
}

// New is the constructor of Node
func New(logger *zap.Logger, opts Options, exporterOpts exporterconfig.Options, slotTickerProvider slotticker.Provider, qbftStorage *qbftstorage.ParticipantStores) *Node {
	selfValidatorStore := opts.ValidatorStore.WithOperatorID(opts.ValidatorOptions.OperatorDataStore.GetOperatorID)

	var (
		feeRecipientCtrl       fee_recipient.RecipientController
		proposalPreparationsFn func() ([]*eth2apiv1.ProposalPreparation, error)
	)
	if !exporterOpts.Enabled {
		feeRecipientController := fee_recipient.NewController(logger, &fee_recipient.ControllerOptions{
			Ctx:                opts.Context,
			BeaconClient:       opts.BeaconNode,
			BeaconConfig:       opts.NetworkConfig.Beacon,
			ValidatorProvider:  selfValidatorStore,
			OperatorDataStore:  opts.ValidatorOptions.OperatorDataStore,
			SlotTickerProvider: slotTickerProvider,
		})
		feeRecipientCtrl = feeRecipientController
		proposalPreparationsFn = feeRecipientController.GetProposalPreparations
	}

	var dutyScheduler *duties.Scheduler
	if shouldRunDutyScheduler(exporterOpts) {
		// Prepare scheduler wiring; in exporter archive mode we swap to AllShares provider,
		// a prefetching beacon adapter, and a no-op executor.
		schedulerBeacon := duties.BeaconNode(opts.BeaconNode)
		validatorProvider := duties.ValidatorProvider(selfValidatorStore)
		dutyExecutor := duties.DutyExecutor(opts.ValidatorController)

		if exporterOpts.Enabled {
			validatorProvider = duties.NewAllSharesProvider(opts.ValidatorStore)
			dutyExecutor = duties.NewNoopExecutor()
			schedulerBeacon = duties.NewPrefetchingBeacon(logger, opts.BeaconNode, opts.NetworkConfig.Beacon, opts.ValidatorStore)
		}

		dutyScheduler = duties.NewScheduler(logger, &duties.SchedulerOptions{
			Ctx:                     opts.Context,
			BeaconNode:              schedulerBeacon,
			ExecutionClient:         opts.ExecutionClient,
			NetworkConfig:           opts.NetworkConfig,
			ValidatorProvider:       validatorProvider,
			ValidatorController:     opts.ValidatorController,
			DutyExecutor:            dutyExecutor,
			IndicesChgCh:            opts.ValidatorController.IndicesChangeChan(),
			ValidatorRegistrationCh: opts.ValidatorController.ValidatorRegistrationChan(),
			ValidatorExitCh:         opts.ValidatorController.ValidatorExitChan(),
			DutyStore:               opts.DutyStore,
			SlotTickerProvider:      slotTickerProvider,
			P2PNetwork:              opts.P2PNetwork,
			ExporterMode:            exporterOpts.Enabled,
		})
	}

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
		dutyScheduler:    dutyScheduler,
		feeRecipientCtrl: feeRecipientCtrl,

		ws:           opts.WS,
		exporterRead: opts.ExporterRead,
	}

	if feeRecipientCtrl != nil {
		// Wire the beacon client to the fee recipient controller
		// This allows the beacon client to pull proposal preparations on reconnect.
		opts.BeaconNode.SetProposalPreparationsProvider(proposalPreparationsFn)

		// Subscribe fee recipient controller to validator controller's change notifications.
		feeRecipientCtrl.SubscribeToFeeRecipientChanges(opts.ValidatorController.FeeRecipientChangeChan())
	}

	return node
}

// Start prepares and starts the main components.
func (n *Node) Start(ctx context.Context) error {
	n.logger.Info("starting operator node")

	// On an early return, defer cancel() signals the early-joined members (the WS serve loop and the
	// scheduler wait) to stop; it doesn't await them — they unwind in the background.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)

	// Plain receive is safe: the WS server takes gctx (via startWSServer), so its Shutdown closes
	// serveErr on cancel and this can't wedge g.Wait().
	if n.ws != nil {
		wsServeErr, err := n.startWSServer(gctx)
		if err != nil {
			return fmt.Errorf("start WS server: %w", err)
		}
		g.Go(func() error {
			if err := <-wsServeErr; err != nil {
				return fmt.Errorf("WS server serve loop exited: %w", err)
			}
			return nil
		})
	}

	// Exporter-standard runs no duties — it just stays up until shutdown. dutyScheduler is nil only for
	// that mode; any other mode reaching the default is a wiring bug.
	switch {
	case n.dutyScheduler != nil:
		if err := n.dutyScheduler.Start(gctx); err != nil {
			return fmt.Errorf("failed to run duty scheduler: %w", err)
		}
		g.Go(func() error {
			// Always nil today (tasks exit on gctx cancel), but propagate any future error rather than
			// swallowing it.
			if err := n.dutyScheduler.Wait(); err != nil {
				return fmt.Errorf("duty scheduler exited with error: %w", err)
			}
			return nil
		})
	case n.exporterOptions.Enabled && n.exporterOptions.Mode == exporterconfig.ModeStandard:
		g.Go(func() error {
			<-gctx.Done()
			return nil
		})
	default:
		return errors.New("duty scheduler is nil for non-exporter-standard node")
	}

	n.validatorsCtrl.StartNetworkHandlers()

	// Call the shared validator initialization path in both modes.
	// Exporter returns no committee validators here but still relies on the common startup flow.
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
			if err := n.net.SubscribeAll(); err != nil {
				return fmt.Errorf("subscribe to all subnets: %w", err)
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

	go n.validatorsCtrl.HandleMetadataUpdates(ctx)
	if n.exporterOptions.Enabled {
		n.logger.Info("exporter mode: skipping fee recipient, validator status reporting and doppelganger services")
	} else {
		go n.feeRecipientCtrl.Start(ctx)
		go n.validatorsCtrl.ReportValidatorStatuses(ctx)

		go func() {
			if err := n.validatorOptions.DoppelgangerHandler.Start(ctx); err != nil {
				n.logger.Error("Doppelganger monitoring exited with error", zap.Error(err))
			}
		}()
	}

	n.logger.Info("operator node has been started", fields.OperatorID(n.validatorOptions.OperatorDataStore.GetOperatorID()))

	return g.Wait()
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
		// In exporter v2 (archive) mode we serve decided queries via exporter core.
		// Fall back to legacy qbft storage when exporter isn't wired.
		if n.exporterRead != nil {
			n.handleDecidedViaExporter(nm)
			break
		}
		h.HandleParticipantsQuery(n.qbftStorage, nm, n.network)
	case api.TypeError:
		h.HandleErrorQuery(nm)
	default:
		h.HandleUnknownQuery(nm)
	}
}

func (n *Node) handleDecidedViaExporter(nm *api.NetworkMessage) {
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

	coreQuery := &exportercore.DecidedsQuery{
		From:    nm.Msg.Filter.From,
		To:      nm.Msg.Filter.To,
		Roles:   []spectypes.BeaconRole{role},
		Indices: []phase0.ValidatorIndex{idx},
	}

	result, errs := n.exporterRead.TraceDecidedsCore(coreQuery)
	participations := wsParticipationsFromCore(result)

	var unexpectedErr error
	filtered := filterOutDutyNotFoundErrors(errs)
	if filtered.ErrorOrNil() != nil {
		for _, e := range filtered.Errors {
			// Preserve legacy WS leniency: treat validation errors as "no messages".
			if isExporterValidationError(e) {
				continue
			}
			unexpectedErr = e
		}
	}

	if len(participations) == 0 {
		if unexpectedErr != nil {
			n.logger.Warn("failed to build participants api data due to exporter errors", zap.Error(unexpectedErr), fields.ValidatorIndex(idx))
			res.Type = api.TypeError
			res.Data = []string{fmt.Sprintf("internal error - could not build response: %v", unexpectedErr)}
		} else {
			// Mirror legacy exporter behavior: empty range returns "no messages" as a decided response.
			res.Data = []string{"no messages"}
		}
		nm.Msg = res
		return
	}

	data, err := api.ParticipantsAPIData(n.network, participations...)
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

func wsParticipationsFromCore(result *exportercore.TraceDecidedsResult) []qbftstorage.Participation {
	out := make([]qbftstorage.Participation, 0)
	if result == nil {
		return out
	}
	for _, p := range result.Participants {
		out = append(out, qbftstorage.Participation{
			ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
				Slot:    p.Slot,
				PubKey:  p.PubKey,
				Signers: p.Signers,
			},
			Role:   p.Role,
			PubKey: p.PubKey,
		})
	}
	return out
}

func isExporterValidationError(err error) bool {
	var ve *exportercore.ValidationError
	return errors.As(err, &ve)
}

// isNotFoundError returns true if the error represents an expected "no duty"
// condition, either from the duty tracer or the underlying exporter store.
// It mirrors the semantics in exporter helpers to ease future refactoring.
func isNotFoundError(err error) bool {
	return errors.Is(err, dutytracer.ErrNotFound) || errors.Is(err, exporterstore.ErrNotFound)
}

// filterOutDutyNotFoundErrors removes not-found duty errors from a multierror,
// returning nil if nothing remains. It mirrors exportercore.filterOutDutyNotFoundErrors
// to make future WS refactoring simpler.
func filterOutDutyNotFoundErrors(e *multierror.Error) *multierror.Error {
	if e == nil || e.ErrorOrNil() == nil {
		return nil
	}
	var filtered *multierror.Error
	for _, err := range e.Errors {
		if err != nil && !isNotFoundError(err) {
			filtered = multierror.Append(filtered, err)
		}
	}
	return filtered
}

// startWSServer starts the WS API server and returns its serve-error channel. A serve failure
// propagates via the channel — bringing the node down gracefully through Close — rather than
// os.Exit-ing from a goroutine. Requires n.ws != nil.
func (n *Node) startWSServer(ctx context.Context) (<-chan error, error) {
	n.logger.Info("starting WS server")

	n.ws.UseQueryHandler(n.handleQueryRequests)

	_, serveErr, err := n.ws.Start(ctx)
	if err != nil {
		return nil, err
	}

	return serveErr, nil
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
