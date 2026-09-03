package operator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	hexporter "github.com/ssvlabs/ssv/api/handlers/exporter"
	hnode "github.com/ssvlabs/ssv/api/handlers/node"
	hvalidators "github.com/ssvlabs/ssv/api/handlers/validators"
	apiserver "github.com/ssvlabs/ssv/api/server"
	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/eth/executionclient"
	exportercore "github.com/ssvlabs/ssv/exporter"
	dutytracer "github.com/ssvlabs/ssv/exporter/dutytracer"
	dutytracestore "github.com/ssvlabs/ssv/exporter/store"
	exporterapi "github.com/ssvlabs/ssv/exporter/v1/api"
	"github.com/ssvlabs/ssv/exporter/v1/api/decided"
	"github.com/ssvlabs/ssv/hprobe"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/operator"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validator/metadata"
	"github.com/ssvlabs/ssv/operator/validators"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// shutdownGraceTimeout bounds the whole graceful teardown — services unwinding plus the node close —
// once the first terminal event has landed. Past it teardown gives up and surfaces the terminal
// cause instead of hanging forever on something ignoring the cancellation convention; the leftovers
// are reaped at process exit. 15s is generous against the designed sub-second unwind and leaves
// room ahead of orchestrator kill grace.
const shutdownGraceTimeout = 15 * time.Second

// buildNode builds the node graph: it validates config up front (before any network I/O, so a
// misconfigured node fails fast), connects the CL/EL clients — binding them to ctx, whose
// cancellation is goclient's only shutdown — and wires everything together via newNode. The returned
// *node owns the resources newNode opened; node.close releases them at teardown.
func buildNode(ctx context.Context, cfg *config, logger *zap.Logger) (*node, error) {
	// Validate the configuration up front, before any network I/O, so a misconfigured node
	// fails fast (e.g. an invalid ProposerDelay is rejected before the beacon client is built,
	// rather than after connecting to the beacon). resolveAndValidate only reads cfg.
	res, err := cfg.resolveAndValidate(logger)
	if err != nil {
		return nil, err
	}

	ssvNetworkConfig, err := setupSSVNetwork(logger, cfg)
	if err != nil {
		return nil, fmt.Errorf("could not setup network: %w", err)
	}

	logger.Info("connecting CL(s)",
		fields.Address(cfg.ConsensusClient.BeaconNodeAddr),
		zap.Bool("with_weighted_attestation_data", cfg.ConsensusClient.WithWeightedAttestationData),
		zap.Bool("with_parallel_submissions", cfg.ConsensusClient.WithParallelSubmissions),
	)

	cliopt, err := goclient.NewOptions(cfg.ConsensusClient, cfg.ProposerDelay)
	if err != nil {
		return nil, startupError{
			err:    fmt.Errorf("failed to create beacon client options: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}
	consensusClient, err := goclient.New(ctx, logger, cliopt)
	if err != nil {
		return nil, startupError{
			err:    fmt.Errorf("failed to create beacon go-client: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}

	networkConfig := &networkconfig.Network{
		SSV:    ssvNetworkConfig,
		Beacon: consensusClient.BeaconConfig(),
	}
	if err := networkConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid network config: %w", err)
	}

	// Log the Boole fork schedule at boot — the SSV-fork analog of the beacon-side "Gloas (ePBS)
	// fork scheduled" log — so operators and tooling can read each node's epoch and cross-check
	// agreement across the fleet. Unscheduled networks pin Forks.Boole to math.MaxUint64, surfaced
	// at DEBUG to flag a net where Boole should be scheduled but isn't.
	if networkConfig.BooleForkScheduled() {
		logger.Info("boole fork scheduled", fields.Epoch(networkConfig.SSV.Forks.Boole))
	} else {
		logger.Debug("boole fork not scheduled")
	}

	var executionAddrList []string
	for _, addr := range strings.Split(cfg.ExecutionClient.Addr, ";") {
		if addr = strings.TrimSpace(addr); addr != "" {
			executionAddrList = append(executionAddrList, addr)
		}
	}
	if len(executionAddrList) == 0 {
		return nil, fmt.Errorf("no execution node address provided")
	}

	logger.Info("connecting EL(s)",
		fields.Addresses(executionAddrList),
		zap.Duration("request_timeout", cfg.ExecutionClient.ConnectionTimeout),
		zap.Uint64("sync_distance_tolerance", cfg.ExecutionClient.SyncDistanceTolerance),
	)

	var executionClient executionclient.Provider
	if len(executionAddrList) == 1 {
		ec, err := executionclient.New(
			ctx,
			executionAddrList[0],
			ssvNetworkConfig.RegistryContractAddr,
			executionclient.WithLogger(logger),
			executionclient.WithReqTimeout(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithSyncDistanceTolerance(cfg.ExecutionClient.SyncDistanceTolerance),
		)
		if err != nil {
			return nil, fmt.Errorf("could not connect to execution client: %w", err)
		}

		executionClient = ec
	} else {
		ec, err := executionclient.NewMulti(
			ctx,
			executionAddrList,
			ssvNetworkConfig.RegistryContractAddr,
			executionclient.WithLoggerMulti(logger),
			executionclient.WithReqTimeoutMulti(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithSyncDistanceToleranceMulti(cfg.ExecutionClient.SyncDistanceTolerance),
		)
		if err != nil {
			return nil, fmt.Errorf("could not connect to execution client: %w", err)
		}

		executionClient = ec
	}

	n, err := newNode(ctx, cfg, logger, res, networkConfig, consensusClient, executionClient)
	if err != nil {
		// newNode failed before node.close() owns the EL client, so close it here.
		_ = executionClient.Close()
		return nil, err
	}

	return n, nil
}

// beaconClient is the beacon-node surface the node consumes: the duty-call interface plus the
// Healthy probe used to register it as a health-prober component. An interface (satisfied by
// *goclient.GoClient) so an in-process smoke test can inject a stub instead of a real beacon
// connection.
type beaconClient interface {
	beaconprotocol.BeaconNode
	doppelganger.BeaconNode // adds ValidatorLiveness, required by doppelganger.NewHandler
	Healthy(context.Context) error
}

var _ beaconClient = (*goclient.GoClient)(nil)

// node bundles the constructed pieces that start() brings up and close() tears down.
type node struct {
	logger *zap.Logger

	cfg            *config
	mode           nodeMode
	usingSSVSigner bool

	db                  basedb.Database
	consensusClient     beaconClient
	executionClient     executionclient.Provider
	networkConfig       *networkconfig.Network
	nodeStorage         operatorstorage.Storage
	operatorDataStore   operatordatastore.OperatorDataStore
	ssvSignerClient     *ssvsigner.Client
	keyManager          ekm.KeyManager
	doppelgangerHandler doppelganger.Provider
	validatorCtrl       *validator.Controller
	metadataSyncer      *metadata.Syncer
	storageMap          *ibftstorage.ParticipantStores
	collector           *dutytracer.Collector
	vrSubmitter         *runner.VRSubmitter
	slotTickerProvider  slotticker.Provider
	p2pNetwork          network.P2PNetwork
	operatorNode        *operator.Node
}

// newNode wires the node's components from config + the injected beacon/EL clients. On any
// error it closes whatever it already opened (db/p2p) via the error-only defers below; on
// success the returned *node owns those closers, released via node.close(). Goroutines wired
// here (the VRSubmitter; in exporter modes the duty-trace collector / slot-pruning) bind to ctx:
// cancel it before Close, so they've stopped using the resources Close releases. It mutates cfg
// in place (filling SSVOptions/P2pNetworkConfig with runtime deps), so a given cfg is single-use.
func newNode(
	ctx context.Context,
	cfg *config,
	logger *zap.Logger,
	res resolved,
	networkConfig *networkconfig.Network,
	consensusClient beaconClient,
	executionClient executionclient.Provider,
) (_ *node, err error) {
	usingSSVSigner := res.usingSSVSigner

	identity, err := resolveOperatorIdentity(ctx, logger, cfg, res)
	if err != nil {
		return nil, err
	}
	operatorPrivKey := identity.privKey
	operatorPrivKeyPEM := identity.privKeyPEM
	ssvSignerClient := identity.ssvSigner
	operatorPubKeyBase64 := identity.pubKeyB64

	cfg.DBOptions.Ctx = ctx
	db, err := openNodeDB(logger, cfg, res, networkConfig.Beacon, operatorPrivKey)
	if err != nil {
		return nil, fmt.Errorf("could not setup db: %w", err)
	}
	// Close the freshly-opened db on assembly failure; on success the returned node owns it.
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()

	nodeStorage, err := operatorstorage.NewNodeStorage(networkConfig.Beacon, logger, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create node storage: %w", err)
	}

	if !res.isExporter() {
		if usingSSVSigner {
			// Ensure the pubkey is saved on first run and never changes afterwards
			if err := ensureOperatorPubKey(nodeStorage, operatorPubKeyBase64); err != nil {
				return nil, fmt.Errorf("could not save base64-encoded operator public key: %w", err)
			}
		} else {
			if err := ensureOperatorPrivateKey(nodeStorage, operatorPrivKey, operatorPrivKeyPEM); err != nil {
				return nil, fmt.Errorf("could not save operator private key: %w", err)
			}
		}

		logger.Info("successfully loaded operator keys", zap.String(fields.FieldPubKey, operatorPubKeyBase64))
	}

	usingLocalEvents := len(cfg.LocalEventsPath) != 0

	if err := validateConfig(nodeStorage, networkConfig.StorageName(), usingLocalEvents, usingSSVSigner, res.isExporter()); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	cfg.P2pNetworkConfig.Ctx = ctx
	operatorDataStore, err := setupOperatorDataStore(nodeStorage, operatorPubKeyBase64)
	if err != nil {
		return nil, err
	}
	validatorProvider := nodeStorage.ValidatorStore().WithOperatorID(operatorDataStore.GetOperatorID)
	var vrSubmitter *runner.VRSubmitter
	if !res.isExporter() {
		vrSubmitter = runner.NewVRSubmitter(logger, networkConfig.Beacon, consensusClient, validatorProvider)
	}

	keyManager, operatorSigner, err := buildKeyManager(ctx, logger, cfg, res, networkConfig.Beacon, db, ssvSignerClient, operatorPrivKey, operatorDataStore)
	if err != nil {
		return nil, err
	}

	cfg.P2pNetworkConfig.NodeStorage = nodeStorage
	cfg.P2pNetworkConfig.OperatorDataStore = operatorDataStore
	cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode
	cfg.P2pNetworkConfig.NetworkConfig = networkConfig

	validatorsMap := validators.New(ctx)

	dutyStore := dutystore.New()
	cfg.SSVOptions.DutyStore = dutyStore

	signatureVerifier := signatureverifier.NewSignatureVerifier(nodeStorage)

	messageValidator := validation.New(
		networkConfig,
		nodeStorage.ValidatorStore(),
		nodeStorage,
		dutyStore,
		signatureVerifier,
		validation.WithLogger(logger),
	)

	cfg.P2pNetworkConfig.MessageValidator = messageValidator

	p2pNetwork, err := setupP2P(ctx, logger, cfg, db, res.isExporter(), operatorPrivKey, ssvSignerClient)
	if err != nil {
		return nil, err
	}
	// Close the freshly-created network on assembly failure; on success the returned node owns it.
	// Close() is robust on partial Setup state.
	defer func() {
		if err != nil {
			_ = p2pNetwork.Close()
		}
	}()

	cfg.SSVOptions.Context = ctx
	cfg.SSVOptions.DB = db
	cfg.SSVOptions.BeaconNode = consensusClient
	cfg.SSVOptions.ExecutionClient = executionClient
	cfg.SSVOptions.NetworkConfig = networkConfig
	cfg.SSVOptions.P2PNetwork = p2pNetwork

	var newDecidedHandler qbftcontroller.NewDecidedHandler
	var decidedStreamPublisherFn func(dutytracer.DecidedInfo)
	var ws exporterapi.WebSocketServer
	if cfg.WsAPIPort != 0 {
		ws = exporterapi.NewWsServer(logger, nil, http.NewServeMux(), cfg.WithPing, fmt.Sprintf(":%d", cfg.WsAPIPort))
		cfg.SSVOptions.WS = ws
		newDecidedHandler = decided.NewStreamPublisher(logger, networkConfig, ws)
		decidedStreamPublisherFn = decided.NewDecidedListener(logger, networkConfig, ws, nodeStorage.ValidatorStore())
	}

	storageRoles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleProposer,
		spectypes.BNRoleSyncCommittee,
		spectypes.BNRoleAggregator,
		spectypes.BNRoleSyncCommitteeContribution,
		spectypes.BNRoleValidatorRegistration,
		spectypes.BNRoleVoluntaryExit,
	}

	storageMap := ibftstorage.NewStores()

	for _, storageRole := range storageRoles {
		s := ibftstorage.New(logger, db, storageRole)
		storageMap.Add(storageRole, s)
	}

	slotTickerProvider := func() slotticker.SlotTicker {
		return slotticker.New(logger, slotticker.Config{
			SlotDuration: networkConfig.SlotDuration,
			GenesisTime:  networkConfig.GenesisTime,
		})
	}

	fixedSubnets, err := networkcommons.SubnetsFromString(cfg.P2pNetworkConfig.Subnets)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fixed subnets: %w", err)
	}

	if res.isExporter() && fixedSubnets == networkcommons.ZeroSubnets {
		fixedSubnets = networkcommons.AllSubnets
	}

	metadataSyncer := metadata.NewSyncer(
		logger,
		networkConfig,
		nodeStorage.Shares(),
		validatorProvider,
		consensusClient,
		fixedSubnets,
		metadata.WithSyncInterval(cfg.SSVOptions.ValidatorOptions.MetadataUpdateInterval),
	)

	// Exporter duty tracing. An invalid EXPORTER_MODE is rejected up front by resolveAndValidate,
	// so res.mode here is always one of the known modes.
	var collector *dutytracer.Collector
	var messageTraceHandler validator.MessageTraceHandler
	var exporterRead *exportercore.Exporter
	switch res.mode {
	case modeExporterArchive:
		logger.Info("exporter mode: archive")
		dstore := &dutytracer.DutyTraceStoreMetrics{
			Store: dutytracestore.New(db),
		}
		collector = dutytracer.New(logger,
			nodeStorage.ValidatorStore(), consensusClient,
			dstore, networkConfig.Beacon, decidedStreamPublisherFn,
			dutyStore)
		messageTraceHandler = collector.Collect

		exporterRead = exportercore.NewExporter(logger, storageMap, collector, nodeStorage.ValidatorStore(), networkConfig)
	case modeExporterStandard:
		logger.Info("exporter mode: standard")
	case modeOperator:
		// not an exporter: no duty-trace collector
	}

	doppelgangerHandler := buildDoppelganger(logger, cfg, res, networkConfig.Beacon, consensusClient, validatorProvider, slotTickerProvider)
	// Assemble the validator controller options in one place: start from the YAML-loaded base and
	// fill in the resolved runtime dependencies, rather than mutating the struct field-by-field
	// across the function.
	valOpts := cfg.SSVOptions.ValidatorOptions
	valOpts.Context = ctx
	valOpts.NetworkConfig = networkConfig
	valOpts.DB = db
	valOpts.Network = p2pNetwork
	valOpts.Beacon = consensusClient
	valOpts.BeaconSigner = keyManager
	valOpts.OperatorSigner = operatorSigner
	valOpts.MessageValidator = messageValidator
	valOpts.ValidatorsMap = validatorsMap
	valOpts.OperatorDataStore = operatorDataStore
	valOpts.RegistryStorage = nodeStorage
	valOpts.ValidatorStore = nodeStorage.ValidatorStore()
	if vrSubmitter != nil {
		// Guarded so the interface field stays a true nil in exporter mode — assigning a nil
		// *VRSubmitter would make it a non-nil typed-nil interface. start() uses the concrete type.
		valOpts.ValidatorRegistrationSubmitter = vrSubmitter
	}
	valOpts.NewDecidedHandler = newDecidedHandler
	valOpts.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place
	valOpts.StorageMap = storageMap
	valOpts.Graffiti = []byte(cfg.Graffiti)
	valOpts.ProposerDelay = cfg.ProposerDelay
	valOpts.ValidatorSyncer = metadataSyncer
	valOpts.ExporterMode = res.isExporter()
	valOpts.MessageTraceHandler = messageTraceHandler
	valOpts.DoppelgangerHandler = doppelgangerHandler
	cfg.SSVOptions.ValidatorOptions = valOpts

	validatorCtrl := validator.NewController(logger, valOpts)
	cfg.SSVOptions.ValidatorController = validatorCtrl
	cfg.SSVOptions.ValidatorStore = nodeStorage.ValidatorStore()

	if ws != nil {
		handler := exporterapi.NewHandler(logger)
		ws.UseQueryHandler(func(nm *exporterapi.NetworkMessage) {
			handler.HandleQueryRequests(storageMap, exporterRead, nodeStorage.ValidatorStore(), networkConfig, nm)
		})
	}

	operatorNode := operator.New(logger, cfg.SSVOptions, cfg.ExporterOptions, slotTickerProvider)

	return &node{
		logger:              logger,
		cfg:                 cfg,
		mode:                res.mode,
		usingSSVSigner:      usingSSVSigner,
		db:                  db,
		consensusClient:     consensusClient,
		executionClient:     executionClient,
		networkConfig:       networkConfig,
		nodeStorage:         nodeStorage,
		operatorDataStore:   operatorDataStore,
		ssvSignerClient:     ssvSignerClient,
		keyManager:          keyManager,
		doppelgangerHandler: doppelgangerHandler,
		validatorCtrl:       validatorCtrl,
		metadataSyncer:      metadataSyncer,
		storageMap:          storageMap,
		collector:           collector,
		vrSubmitter:         vrSubmitter,
		slotTickerProvider:  slotTickerProvider,
		p2pNetwork:          p2pNetwork,
		operatorNode:        operatorNode,
	}, nil
}

// close tears down the node's resources: validatorCtrl.Stop() (the controller's ttlcache cleanup
// loops, which aren't ctx-bound), then the CLI-owned resources (p2p network and execution client,
// then the db that p2p depends on). The ctx given to newNode must be canceled first, so the
// ctx-bound goroutines have stopped using these resources. newNode is the only constructor, so all
// these fields are always set.
func (n *node) close() error {
	n.validatorCtrl.Stop()

	var errs []error
	if err := n.p2pNetwork.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close p2p network: %w", err))
	}
	if err := n.executionClient.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close execution client: %w", err))
	}
	if err := n.db.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close db: %w", err))
	}
	return errors.Join(errs...)
}

// start brings the node up: it runs the synchronous bring-up steps and registers every long-lived
// service via spawn, returning once the node is up (nil) or a bring-up step has failed (that
// error). Everything binds to ctx — when it dies, bring-up aborts mid-step and the services stop;
// the services' terminal errors travel through spawn, not through the return value.
func (n *node) start(ctx context.Context, spawn func(func() error)) error {
	// Background services wired (but not started) by newNode, mode-dependent.
	if n.vrSubmitter != nil {
		spawn(func() error { n.vrSubmitter.Start(ctx); return nil })
	}
	if n.collector != nil {
		spawn(func() error { n.collector.Start(ctx, n.slotTickerProvider); return nil })
	}
	if n.mode == modeExporterStandard {
		startSlotPruning(ctx, spawn, n.storageMap, n.slotTickerProvider, n.networkConfig.EstimatedCurrentSlot(), n.cfg.ExporterOptions.RetainSlots)
	}

	if n.cfg.MetricsAPIPort > 0 {
		metricsHandler := metrics.NewHandler(n.logger, n.db, n.cfg.EnableProfile, n.operatorNode)
		_, metricsServeErr, err := metricsHandler.Start(ctx, http.NewServeMux(), fmt.Sprintf(":%d", n.cfg.MetricsAPIPort))
		if err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
		// Plain receive is safe: the server's ctx-bound Shutdown closes serveErr on cancel, so this
		// service can't wedge teardown.
		spawn(func() error {
			if err := <-metricsServeErr; err != nil {
				return fmt.Errorf("metrics server serve loop exited: %w", err)
			}
			return nil
		})
	}

	healthProber := hprobe.NewHealthProber(n.logger)
	healthProber.AddComponent(clComponentName, n.consensusClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	healthProber.AddComponent(elComponentName, n.executionClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	if err := ensureComponentsHealthy(ctx, n.logger, healthProber); err != nil {
		return err
	}

	eventSyncer, startOngoingSync, err := syncContractEvents(
		ctx,
		n.logger,
		n.cfg,
		n.executionClient,
		n.validatorCtrl,
		n.networkConfig,
		n.nodeStorage,
		n.operatorDataStore,
		n.keyManager,
		n.doppelgangerHandler,
	)
	if err != nil {
		return err
	}
	if startOngoingSync != nil {
		healthProber.AddComponent(eventSyncerComponentName, eventSyncer, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
		spawn(func() error {
			return startOngoingSync(ctx)
		})
	}

	if _, err := n.metadataSyncer.SyncAll(ctx); err != nil {
		return fmt.Errorf("failed to sync metadata on startup: %w", err)
	}

	if n.usingSSVSigner {
		if err := ensureNoMissingKeys(ctx, n.nodeStorage, n.operatorDataStore, n.ssvSignerClient); err != nil {
			return err
		}
	}

	n.applyDynamicMaxPeers()

	if err := n.startNetwork(healthProber); err != nil {
		return err
	}
	// After startNetwork, so the prober's components (CL/EL, event-syncer, p2p) are all registered.
	spawn(func() error {
		return startHealthProber(ctx, n.logger, healthProber)
	})

	if n.cfg.SSVAPIPort > 0 {
		warnIfSSVAPIAddressUnset(n.logger, n.cfg.SSVAPIAddress, n.cfg.SSVAPIPort)
		apiServer := apiserver.New(
			n.logger,
			net.JoinHostPort(n.cfg.SSVAPIAddress, strconv.Itoa(n.cfg.SSVAPIPort)),
			hnode.NewNode(
				// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
				[]string{fmt.Sprintf("tcp://%s:%d", n.cfg.P2pNetworkConfig.HostAddress, n.cfg.P2pNetworkConfig.TCPPort), fmt.Sprintf("udp://%s:%d", n.cfg.P2pNetworkConfig.HostAddress, n.cfg.P2pNetworkConfig.UDPPort)},
				n.p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
				n.p2pNetwork.(p2pv1.HostProvider).Host().Network(),
				n.p2pNetwork,
				n.operatorDataStore,
				n.networkConfig.CurrentDomainType,
				healthProber,
				clComponentName,
				elComponentName,
				eventSyncerComponentName,
			),
			&hvalidators.Validators{
				Shares: n.nodeStorage.Shares(),
			},
			hexporter.NewExporter(n.logger, n.storageMap, n.collector, n.nodeStorage.ValidatorStore(), n.networkConfig),
			n.mode == modeExporterArchive,
		)
		_, apiServeErr, err := apiServer.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start API server: %w", err)
		}
		spawn(func() error {
			if err := <-apiServeErr; err != nil {
				return fmt.Errorf("API server serve loop exited: %w", err)
			}
			return nil
		})
	}

	spawn(func() error {
		if err := n.operatorNode.Start(ctx); err != nil {
			return fmt.Errorf("failed to start SSV node: %w", err)
		}
		return nil
	})

	return nil
}

// applyDynamicMaxPeers raises the configured MaxPeers to match the number of subnets the operator is
// subscribed to, so a default sized for small operators doesn't starve a large one for peers. No-op
// unless DynamicMaxPeers is enabled; must run before startNetwork so the network comes up with the
// raised value.
// TODO: use OperatorCommittees when it's fixed.
func (n *node) applyDynamicMaxPeers() {
	if !n.cfg.P2pNetworkConfig.DynamicMaxPeers {
		return
	}
	var (
		baseMaxPeers        = 60
		maxPeersLimit       = n.cfg.P2pNetworkConfig.DynamicMaxPeersLimit
		idealPeersPerSubnet = 3
	)
	start := time.Now()
	myValidators := n.nodeStorage.ValidatorStore().OperatorValidators(n.operatorDataStore.GetOperatorID())
	// Post-Boole-fork committees live on Boole subnets; from scheduling through the transition
	// both the Alan and Boole subnets are in play, so budget peers for both. With no fork
	// scheduled (Forks.Boole pinned to MaxUint64) only Alan subnets count — scheduling the fork
	// ships in a release, so a restart re-tallies before Boole subnets ever matter.
	booleFork := n.networkConfig.BooleForkAtSlot(n.networkConfig.EstimatedCurrentSlot())
	myActiveSubnets := operatorActiveSubnets(myValidators, n.networkConfig.BooleForkScheduled(), booleFork)
	idealMaxPeers := min(baseMaxPeers+idealPeersPerSubnet*myActiveSubnets, maxPeersLimit)
	if n.cfg.P2pNetworkConfig.MaxPeers < idealMaxPeers {
		n.logger.Warn("increasing MaxPeers to match the operator's subscribed subnets",
			zap.Int("old_max_peers", n.cfg.P2pNetworkConfig.MaxPeers),
			zap.Int("new_max_peers", idealMaxPeers),
			zap.Int("subscribed_subnets", myActiveSubnets),
			fields.Took(time.Since(start)),
		)
		n.cfg.P2pNetworkConfig.MaxPeers = idealMaxPeers
	}
}

// operatorActiveSubnets returns the number of distinct committee subnets the operator's validators
// participate in. Boole and Alan subnets are tallied in separate bitmaps: they are independent gossip
// topics (/ssv/<net>/boole/<n> vs ssv.v2.<n>) even when their subnet numbers coincide, so a shared
// bitmap would under-count on collision. Boole subnets are only counted once the fork is scheduled
// (otherwise MaxPeers would be overprovisioned on production networks with no fork set); Alan subnets
// are counted until the fork activates. BooleCommitteeSubnet may return UnknownSubnetId for an
// empty committee, but Set drops out-of-range indices, so it never inflates the count.
func operatorActiveSubnets(validators []*ssvtypes.SSVShare, booleScheduled, booleFork bool) int {
	var booleSubnets, alanSubnets networkcommons.Subnets
	for _, v := range validators {
		if booleScheduled {
			booleSubnets.Set(v.BooleCommitteeSubnet())
		}
		if !booleFork {
			alanSubnets.Set(v.AlanCommitteeSubnet())
		}
	}
	return booleSubnets.ActiveCount() + alanSubnets.ActiveCount()
}

// startNetwork wires validator stats into the p2p layer, then sets up + starts the network and
// registers it with the health prober. Split out of start() so this DynamicMaxPeers-independent
// invariant can be unit-tested with a stub network.
func (n *node) startNetwork(healthProber *hprobe.HealthProber) error {
	// These run unconditionally. Gating them on DynamicMaxPeers (a long-standing bug) left
	// DynamicMaxPeers=false nodes with a network that was never Setup/Start'd — operator.Node.Start
	// then failed Subscribe* with ErrNetworkIsNotReady — and with a nil GetValidatorStats, which
	// made pubsub peer scoring silently fall back to fake hard-coded stats. GetValidatorStats must
	// be set before Setup(), which reads it via setupPubsub.
	n.cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
		return n.validatorCtrl.GetValidatorStats()
	}
	if err := n.p2pNetwork.Setup(); err != nil {
		return fmt.Errorf("failed to setup network: %w", err)
	}
	if err := n.p2pNetwork.Start(); err != nil {
		return fmt.Errorf("failed to start network: %w", err)
	}
	healthProber.AddComponent(p2pComponentName, n.p2pNetwork.(p2pv1.HealthChecker), proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	return nil
}

func setupOperatorDataStore(
	nodeStorage operatorstorage.Storage,
	base64PubKey string,
) (operatordatastore.OperatorDataStore, error) {
	if base64PubKey == "" {
		// Exporter runs without operator identity, so initialize an empty datastore
		// instead of looking up operator data by pubkey.
		return operatordatastore.New(&registrystorage.OperatorData{}), nil
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, base64PubKey)
	if err != nil {
		return nil, fmt.Errorf("could not get operator data by public key: %w", err)
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: base64PubKey,
		}
	}
	if operatorData == nil {
		return nil, fmt.Errorf("invalid operator data in database: nil")
	}

	return operatordatastore.New(operatorData), nil
}

// startSlotPruning runs the one-time initial slot GC (synchronously, in parallel across stores) and
// then spawns a per-store background cleanup loop joined to the supervised lifecycle.
func startSlotPruning(ctx context.Context, spawn func(func() error), stores *ibftstorage.ParticipantStores, slotTickerProvider slotticker.Provider, slot phase0.Slot, retain uint64) {
	threshold := slot - phase0.Slot(retain)

	// One-time initial GC: prune each store in parallel, wait for all.
	var wg sync.WaitGroup
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		wg.Go(func() { store.Prune(ctx, threshold) })
		return nil
	})
	wg.Wait()

	// Background per-store cleanup on every tick.
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		spawn(func() error {
			store.PruneContinuously(ctx, slotTickerProvider, phase0.Slot(retain))
			return nil
		})
		return nil
	})
}

// buildDoppelganger returns the node's doppelganger-protection provider: a no-op for exporter nodes
// (and for operator nodes with protection disabled), or a real handler when protection is enabled.
func buildDoppelganger(
	logger *zap.Logger,
	cfg *config,
	res resolved,
	beaconConfig *networkconfig.Beacon,
	beaconNode doppelganger.BeaconNode,
	validatorProvider doppelganger.ValidatorProvider,
	slotTickerProvider slotticker.Provider,
) doppelganger.Provider {
	if res.isExporter() {
		return doppelganger.NoOpHandler{}
	}
	if cfg.EnableDoppelgangerProtection {
		handler := doppelganger.NewHandler(&doppelganger.Options{
			BeaconConfig:       beaconConfig,
			BeaconNode:         beaconNode,
			ValidatorProvider:  validatorProvider,
			SlotTickerProvider: slotTickerProvider,
			Logger:             logger,
		})
		logger.Info("Doppelganger protection enabled.")
		return handler
	}
	logger.Info("Doppelganger protection disabled.")
	return doppelganger.NoOpHandler{}
}
