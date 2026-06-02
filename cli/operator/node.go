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
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/exporter/api/decided"
	dutytracestore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter2"
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
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validator/metadata"
	"github.com/ssvlabs/ssv/operator/validators"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func runNode(ctx context.Context, cfg *config, logger *zap.Logger) error {
	// runNode owns the parent ctx for the clients it builds directly (goclient, execution) before
	// newNode exists. goclient has no Close() of its own — ctx cancellation is its only shutdown —
	// so cancel on return to release it once the node has stopped and we unwind.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Validate the configuration up front, before any network I/O, so a misconfigured node
	// fails fast (e.g. an invalid ProposerDelay is rejected before the beacon client is built,
	// rather than after connecting to the beacon). resolveAndValidate only reads cfg.
	res, err := cfg.resolveAndValidate(logger)
	if err != nil {
		return err
	}

	ssvNetworkConfig, err := setupSSVNetwork(logger, cfg)
	if err != nil {
		return fmt.Errorf("could not setup network: %w", err)
	}

	logger.Info("connecting CL(s)",
		fields.Address(cfg.ConsensusClient.BeaconNodeAddr),
		zap.Bool("with_weighted_attestation_data", cfg.ConsensusClient.WithWeightedAttestationData),
		zap.Bool("with_parallel_submissions", cfg.ConsensusClient.WithParallelSubmissions),
	)

	cliopt, err := goclient.NewOptions(cfg.ConsensusClient, cfg.ProposerDelay)
	if err != nil {
		return startupError{
			err:    fmt.Errorf("failed to create beacon client options: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}
	consensusClient, err := goclient.New(ctx, logger, cliopt)
	if err != nil {
		return startupError{
			err:    fmt.Errorf("failed to create beacon go-client: %w", err),
			fields: []zap.Field{fields.Address(cfg.ConsensusClient.BeaconNodeAddr)},
		}
	}

	networkConfig := &networkconfig.Network{
		SSV:    ssvNetworkConfig,
		Beacon: consensusClient.BeaconConfig(),
	}

	var executionAddrList []string
	for _, addr := range strings.Split(cfg.ExecutionClient.Addr, ";") {
		if addr = strings.TrimSpace(addr); addr != "" {
			executionAddrList = append(executionAddrList, addr)
		}
	}
	if len(executionAddrList) == 0 {
		return fmt.Errorf("no execution node address provided")
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
			return fmt.Errorf("could not connect to execution client: %w", err)
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
			return fmt.Errorf("could not connect to execution client: %w", err)
		}

		executionClient = ec
	}

	n, err := newNode(ctx, cfg, logger, res, networkConfig, consensusClient, executionClient)
	if err != nil {
		// newNode failed before node.Close() owns the EL client, so close it here.
		_ = executionClient.Close()
		return err
	}
	defer func() {
		if err := n.Close(); err != nil {
			logger.Error("could not cleanly close node", zap.Error(err))
		}
	}()

	return n.start()
}

// beaconClient is the beacon-node surface that newNode()/start() consume: the duty-call
// interface plus the Healthy probe used to register it as a health-prober component. runNode()
// passes in a *goclient.GoClient as this interface, so an in-process smoke test can inject a
// stub instead of a real beacon connection.
type beaconClient interface {
	beaconprotocol.BeaconNode
	doppelganger.BeaconNode // adds ValidatorLiveness, required by doppelganger.NewHandler
	Healthy(context.Context) error
}

var _ beaconClient = (*goclient.GoClient)(nil)

// node bundles the constructed pieces that start() runs and Close() tears down.
type node struct {
	ctx    context.Context
	cancel context.CancelFunc

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
	p2pNetwork          network.P2PNetwork
	operatorNode        *operator.Node
}

// newNode wires the node's components from config + the injected beacon/EL clients. On any
// error it closes whatever it already opened (db/p2p) via the error-only defers below; on
// success the returned *node owns those closers (runNode() defers n.Close()). It mutates cfg
// in place (filling SSVOptions/P2pNetworkConfig with runtime deps), so a given cfg is single-use.
func newNode(ctx context.Context, cfg *config, logger *zap.Logger, res resolved, networkConfig *networkconfig.Network, consensusClient beaconClient, executionClient executionclient.Provider) (_ *node, err error) {
	usingSSVSigner := res.usingSSVSigner

	// Derive the node's own ctx from the parent so node.Close() can stop the ctx-bound goroutines
	// wired below — the VRSubmitter, plus (in exporter modes) the duty-trace collector / slot-pruning.
	// On assembly failure cancel here so a half-wired node doesn't leak the ctx-bound ones; on success
	// Close() owns it.
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

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
	var validatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter
	if !res.isExporter() {
		validatorRegistrationSubmitter = runner.NewVRSubmitter(ctx, logger, networkConfig.Beacon, consensusClient, validatorProvider)
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
	if cfg.WsAPIPort != 0 {
		ws := exporterapi.NewWsServer(ctx, logger, nil, http.NewServeMux(), cfg.WithPing)
		cfg.SSVOptions.WS = ws
		cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
		newDecidedHandler = decided.NewStreamPublisher(logger, networkConfig.DomainType, ws)
		decidedStreamPublisherFn = decided.NewDecidedListener(logger, networkConfig.DomainType, ws, nodeStorage.ValidatorStore())
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

	if res.mode == modeExporterStandard {
		retain := cfg.ExporterOptions.RetainSlots
		threshold := networkConfig.EstimatedCurrentSlot()
		initSlotPruning(ctx, storageMap, slotTickerProvider, threshold, retain)
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
		nodeStorage.Shares(),
		validatorProvider,
		consensusClient,
		fixedSubnets,
		metadata.WithSyncInterval(cfg.SSVOptions.ValidatorOptions.MetadataUpdateInterval),
	)

	// Exporter duty tracing. An invalid EXPORTER_MODE is rejected up front by resolveAndValidate,
	// so res.mode here is always one of the known modes.
	var collector *dutytracer.Collector
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

		go collector.Start(ctx, slotTickerProvider)
		cfg.SSVOptions.ExporterRead = exporter2.NewExporter(logger, storageMap, collector, nodeStorage.ValidatorStore())
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
	valOpts.ValidatorRegistrationSubmitter = validatorRegistrationSubmitter
	valOpts.NewDecidedHandler = newDecidedHandler
	valOpts.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place
	valOpts.StorageMap = storageMap
	valOpts.Graffiti = []byte(cfg.Graffiti)
	valOpts.ProposerDelay = cfg.ProposerDelay
	valOpts.ValidatorSyncer = metadataSyncer
	valOpts.DutyTraceCollector = collector
	valOpts.DoppelgangerHandler = doppelgangerHandler
	cfg.SSVOptions.ValidatorOptions = valOpts

	validatorCtrl := validator.NewController(logger, valOpts, cfg.ExporterOptions)
	cfg.SSVOptions.ValidatorController = validatorCtrl
	cfg.SSVOptions.ValidatorStore = nodeStorage.ValidatorStore()

	operatorNode := operator.New(logger, cfg.SSVOptions, cfg.ExporterOptions, slotTickerProvider, storageMap)

	return &node{
		ctx:                 ctx,
		cancel:              cancel,
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
		p2pNetwork:          p2pNetwork,
		operatorNode:        operatorNode,
	}, nil
}

// Close stops the node's background work and tears down its resources. It cancels the node's ctx
// (stopping the VRSubmitter and, in exporter modes, the duty-trace collector / slot-pruning) and
// calls validatorCtrl.Stop() (the controller's ttlcache cleanup loops, which aren't ctx-bound), then
// closes the CLI-owned resources (p2p network and execution client, then the db that p2p depends on).
// newNode is the only constructor, so all these fields are always set.
func (n *node) Close() error {
	n.cancel()
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

// start launches the node's long-lived services (metrics + SSV API servers, the health
// prober, contract-event sync) and blocks on operatorNode.Start until the node's ctx is canceled.
// NOTE: the metrics/SSV-API server goroutines call logger.Fatal on failure — crashing the
// process and bypassing Close() — a candidate for errgroup-based coordinated shutdown.
func (n *node) start() error {
	if n.cfg.MetricsAPIPort > 0 {
		go func() {
			metricsHandler := metrics.NewHandler(n.logger, n.db, n.cfg.EnableProfile, n.operatorNode)
			if err := metricsHandler.Start(http.NewServeMux(), fmt.Sprintf(":%d", n.cfg.MetricsAPIPort)); err != nil {
				n.logger.Fatal("failed to serve metrics", zap.Error(err))
			}
		}()
	}

	healthProber := hprobe.NewHealthProber(n.logger)
	healthProber.AddComponent(clComponentName, n.consensusClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	healthProber.AddComponent(elComponentName, n.executionClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	if err := ensureComponentsHealthy(n.ctx, n.logger, healthProber); err != nil {
		return err
	}

	eventSyncer, err := syncContractEvents(
		n.ctx,
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
	if len(n.cfg.LocalEventsPath) == 0 {
		healthProber.AddComponent(eventSyncerComponentName, eventSyncer, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	}

	go startHealthProber(n.ctx, n.logger, healthProber)

	if _, err := n.metadataSyncer.SyncAll(n.ctx); err != nil {
		return fmt.Errorf("failed to sync metadata on startup: %w", err)
	}

	if n.usingSSVSigner {
		if err := ensureNoMissingKeys(n.ctx, n.nodeStorage, n.operatorDataStore, n.ssvSignerClient); err != nil {
			return err
		}
	}

	// Increase MaxPeers if the operator is subscribed to many subnets.
	// TODO: use OperatorCommittees when it's fixed.
	if n.cfg.P2pNetworkConfig.DynamicMaxPeers {
		var (
			baseMaxPeers        = 60
			maxPeersLimit       = n.cfg.P2pNetworkConfig.DynamicMaxPeersLimit
			idealPeersPerSubnet = 3
		)
		start := time.Now()
		myValidators := n.nodeStorage.ValidatorStore().OperatorValidators(n.operatorDataStore.GetOperatorID())
		mySubnets := networkcommons.Subnets{}
		myActiveSubnets := 0
		for _, v := range myValidators {
			subnet := networkcommons.CommitteeSubnet(v.CommitteeID())
			if !mySubnets.IsSet(subnet) {
				mySubnets.Set(subnet)
				myActiveSubnets++
			}
		}
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

	if err := n.startNetwork(healthProber); err != nil {
		return err
	}

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
				healthProber,
				clComponentName,
				elComponentName,
				eventSyncerComponentName,
			),
			&hvalidators.Validators{
				Shares: n.nodeStorage.Shares(),
			},
			hexporter.NewExporter(n.logger, n.storageMap, n.collector, n.nodeStorage.ValidatorStore()),
			n.mode == modeExporterArchive,
		)
		go func() {
			err := apiServer.Run()
			if err != nil {
				n.logger.Fatal("failed to start API server", zap.Error(err))
			}
		}()
	}
	if err := n.operatorNode.Start(n.ctx); err != nil {
		return fmt.Errorf("failed to start SSV node: %w", err)
	}

	return nil
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

func initSlotPruning(ctx context.Context, stores *ibftstorage.ParticipantStores, slotTickerProvider slotticker.Provider, slot phase0.Slot, retain uint64) {
	var wg sync.WaitGroup

	threshold := slot - phase0.Slot(retain)

	// async perform initial slot gc
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Prune(ctx, threshold)
		}()
		return nil
	})

	wg.Wait()

	// start background job for removing old slots on every tick
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		go store.PruneContinuously(ctx, slotTickerProvider, phase0.Slot(retain))
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
