package operator

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	hexporter "github.com/ssvlabs/ssv/api/handlers/exporter"
	hnode "github.com/ssvlabs/ssv/api/handlers/node"
	hvalidators "github.com/ssvlabs/ssv/api/handlers/validators"
	apiserver "github.com/ssvlabs/ssv/api/server"
	"github.com/ssvlabs/ssv/hprobe"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"

	"github.com/ssvlabs/ssv/ekmadapter"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/eth/executionclient"
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/exporter/api/decided"
	dutytracestore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter2"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
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
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// beaconClient is the beacon-node surface that assemble()/start() consume: the duty-call
// interface plus the Healthy probe used to register the client as a health-prober component.
// run() builds a *goclient.GoClient (which additionally exposes BeaconConfig(), used only by
// run() when constructing networkConfig) and passes it in as this interface, which lets an
// in-process smoke test inject a stub instead of a real beacon connection.
type beaconClient interface {
	beaconprotocol.BeaconNode
	doppelganger.BeaconNode // adds ValidatorLiveness, required by doppelganger.NewHandler
	Healthy(context.Context) error
}

var _ beaconClient = (*goclient.GoClient)(nil)

// assembled is the set of constructed node components that start() runs and Close() tears down.
// Construction intermediates (signature verifier, duty store, ekm adapter, …) stay locals in
// assemble(); only what start()/Close() read lives here.
type assembled struct {
	logger              *zap.Logger
	cfg                 *config
	mode                nodeMode
	usingSSVSigner      bool
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

// assemble wires the node's components from config + the injected beacon/EL clients. On any
// error it closes whatever it already opened (db/p2p) via the error-only defers below; on
// success the returned *assembled owns those closers (run() defers a.Close()).
func assemble(ctx context.Context, cfg *config, logger *zap.Logger, res resolved, networkConfig *networkconfig.Network, consensusClient beaconClient, executionClient executionclient.Provider) (_ *assembled, err error) {
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
	db, err := openNodeDB(logger, cfg, networkConfig.Beacon, operatorPrivKey)
	if err != nil {
		return nil, fmt.Errorf("could not setup db: %w", err)
	}
	// On assembly failure after the db is opened, close it here; on success the returned
	// assembled owns it (run defers a.Close()).
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()

	nodeStorage, err := operatorstorage.NewNodeStorage(networkConfig.Beacon, logger, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create node storage: %w", err)
	}

	if !cfg.ExporterOptions.Enabled {
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

	if err := validateConfig(nodeStorage, networkConfig.StorageName(), usingLocalEvents, usingSSVSigner, cfg.ExporterOptions.Enabled); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	cfg.P2pNetworkConfig.Ctx = ctx
	operatorDataStore, err := setupOperatorDataStore(nodeStorage, operatorPubKeyBase64)
	if err != nil {
		return nil, err
	}
	validatorProvider := nodeStorage.ValidatorStore().WithOperatorID(operatorDataStore.GetOperatorID)
	var validatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter
	if !cfg.ExporterOptions.Enabled {
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

	p2pNetwork, err := setupP2P(ctx, logger, cfg, db, cfg.ExporterOptions.Enabled, operatorPrivKey, ssvSignerClient)
	if err != nil {
		return nil, err
	}
	// On assembly failure after the network is created, close it here; on success the returned
	// assembled owns it (run defers a.Close()). Close() is robust on partial Setup state.
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
		threshold := cfg.SSVOptions.NetworkConfig.EstimatedCurrentSlot()
		initSlotPruning(ctx, storageMap, slotTickerProvider, threshold, retain)
	}

	fixedSubnets, err := networkcommons.SubnetsFromString(cfg.P2pNetworkConfig.Subnets)
	if err != nil {
		return nil, fmt.Errorf("failed to parse fixed subnets: %w", err)
	}

	if cfg.ExporterOptions.Enabled && fixedSubnets == networkcommons.ZeroSubnets {
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

	var doppelgangerHandler doppelganger.Provider
	if cfg.ExporterOptions.Enabled {
		doppelgangerHandler = doppelganger.NoOpHandler{}
	} else if cfg.EnableDoppelgangerProtection {
		doppelgangerHandler = doppelganger.NewHandler(&doppelganger.Options{
			BeaconConfig:       networkConfig.Beacon,
			BeaconNode:         consensusClient,
			ValidatorProvider:  validatorProvider,
			SlotTickerProvider: slotTickerProvider,
			Logger:             logger,
		})
		logger.Info("Doppelganger protection enabled.")
	} else {
		doppelgangerHandler = doppelganger.NoOpHandler{}
		logger.Info("Doppelganger protection disabled.")
	}
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

	return &assembled{
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

// Close tears down the CLI-owned resources (p2p network, then the db it depends on). Safe to
// call on a partially-assembled node (each field is nil-guarded).
func (a *assembled) Close() error {
	var errs []error
	if a.p2pNetwork != nil {
		if err := a.p2pNetwork.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close p2p network: %w", err))
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close db: %w", err))
		}
	}
	return errors.Join(errs...)
}

// start launches the node's long-lived services (metrics + SSV API servers, the health
// prober, contract-event sync) and blocks on operatorNode.Start until ctx is canceled.
// NOTE: the metrics/SSV-API server goroutines call logger.Fatal on failure — crashing the
// process and bypassing Close(). This is preserved from the original startup path and is a
// candidate for coordinated (errgroup-based) shutdown in a follow-up.
func (a *assembled) start(ctx context.Context) error {
	if a.cfg.MetricsAPIPort > 0 {
		go func() {
			metricsHandler := metrics.NewHandler(a.logger, a.db, a.cfg.EnableProfile, a.operatorNode)
			if err := metricsHandler.Start(http.NewServeMux(), fmt.Sprintf(":%d", a.cfg.MetricsAPIPort)); err != nil {
				a.logger.Fatal("failed to serve metrics", zap.Error(err))
			}
		}()
	}

	healthProber := hprobe.NewHealthProber(a.logger)
	healthProber.AddComponent(clComponentName, a.consensusClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	healthProber.AddComponent(elComponentName, a.executionClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	if err := ensureComponentsHealthy(ctx, a.logger, healthProber); err != nil {
		return err
	}

	eventSyncer, err := syncContractEvents(
		ctx,
		a.logger,
		a.cfg,
		a.executionClient,
		a.validatorCtrl,
		a.networkConfig,
		a.nodeStorage,
		a.operatorDataStore,
		a.keyManager,
		a.doppelgangerHandler,
	)
	if err != nil {
		return err
	}
	if len(a.cfg.LocalEventsPath) == 0 {
		healthProber.AddComponent(eventSyncerComponentName, eventSyncer, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	}
	go startHealthProber(ctx, a.logger, healthProber)

	if _, err := a.metadataSyncer.SyncAll(ctx); err != nil {
		return fmt.Errorf("failed to sync metadata on startup: %w", err)
	}

	if a.usingSSVSigner {
		if err := ensureNoMissingKeys(ctx, a.nodeStorage, a.operatorDataStore, a.ssvSignerClient); err != nil {
			return err
		}
	}

	// Increase MaxPeers if the operator is subscribed to many subnets.
	// TODO: use OperatorCommittees when it's fixed.
	if a.cfg.P2pNetworkConfig.DynamicMaxPeers {
		var (
			baseMaxPeers        = 60
			maxPeersLimit       = a.cfg.P2pNetworkConfig.DynamicMaxPeersLimit
			idealPeersPerSubnet = 3
		)
		start := time.Now()
		myValidators := a.nodeStorage.ValidatorStore().OperatorValidators(a.operatorDataStore.GetOperatorID())
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
		if a.cfg.P2pNetworkConfig.MaxPeers < idealMaxPeers {
			a.logger.Warn("increasing MaxPeers to match the operator's subscribed subnets",
				zap.Int("old_max_peers", a.cfg.P2pNetworkConfig.MaxPeers),
				zap.Int("new_max_peers", idealMaxPeers),
				zap.Int("subscribed_subnets", myActiveSubnets),
				fields.Took(time.Since(start)),
			)
			a.cfg.P2pNetworkConfig.MaxPeers = idealMaxPeers
		}
	}

	if err := a.startNetwork(healthProber); err != nil {
		return err
	}

	if a.cfg.SSVAPIPort > 0 {
		warnIfSSVAPIAddressUnset(a.logger, a.cfg.SSVAPIAddress, a.cfg.SSVAPIPort)
		apiServer := apiserver.New(
			a.logger,
			net.JoinHostPort(a.cfg.SSVAPIAddress, strconv.Itoa(a.cfg.SSVAPIPort)),
			hnode.NewNode(
				// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
				[]string{fmt.Sprintf("tcp://%s:%d", a.cfg.P2pNetworkConfig.HostAddress, a.cfg.P2pNetworkConfig.TCPPort), fmt.Sprintf("udp://%s:%d", a.cfg.P2pNetworkConfig.HostAddress, a.cfg.P2pNetworkConfig.UDPPort)},
				a.p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
				a.p2pNetwork.(p2pv1.HostProvider).Host().Network(),
				a.p2pNetwork,
				healthProber,
				clComponentName,
				elComponentName,
				eventSyncerComponentName,
			),
			&hvalidators.Validators{
				Shares: a.nodeStorage.Shares(),
			},
			hexporter.NewExporter(a.logger, a.storageMap, a.collector, a.nodeStorage.ValidatorStore()),
			a.mode == modeExporterArchive,
		)
		go func() {
			err := apiServer.Run()
			if err != nil {
				a.logger.Fatal("failed to start API server", zap.Error(err))
			}
		}()
	}
	if err := a.operatorNode.Start(a.cfg.SSVOptions.Context); err != nil {
		return fmt.Errorf("failed to start SSV node: %w", err)
	}

	return nil
}

// startNetwork wires validator stats into the p2p layer, then sets up + starts the network and
// registers it with the health prober. Split out of start() so this DynamicMaxPeers-independent
// invariant can be unit-tested with a stub network.
func (a *assembled) startNetwork(healthProber *hprobe.HealthProber) error {
	// Wire validator stats into pubsub peer scoring, then set up and start the p2p network.
	// These run unconditionally: gating them on DynamicMaxPeers (a long-standing bug) left
	// DynamicMaxPeers=false nodes with a network that was never Setup/Start'd — operator.Node.Start
	// then failed Subscribe* with ErrNetworkIsNotReady — and with a nil GetValidatorStats, which
	// makes pubsub peer scoring silently fall back to fake hard-coded stats. GetValidatorStats
	// must be set before Setup(), which reads it via setupPubsub.
	a.cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
		return a.validatorCtrl.GetValidatorStats()
	}
	if err := a.p2pNetwork.Setup(); err != nil {
		return fmt.Errorf("failed to setup network: %w", err)
	}
	if err := a.p2pNetwork.Start(); err != nil {
		return fmt.Errorf("failed to start network: %w", err)
	}
	healthProber.AddComponent(p2pComponentName, a.p2pNetwork.(p2pv1.HealthChecker), proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	return nil
}

// operatorIdentity is the operator's signing material resolved from config. In exporter mode it is
// empty (no signing); otherwise it carries either the private key (keystore / arg modes) or the
// ssv-signer client (remote mode), plus the base64 public key persisted on first run.
type operatorIdentity struct {
	privKey    keys.OperatorPrivateKey
	privKeyPEM string
	ssvSigner  *ssvsigner.Client
	pubKeyB64  string
}

// resolveOperatorIdentity loads the operator signing material for the node's mode: exporter nodes
// don't sign (empty identity); operator nodes resolve it from ssv-signer, a keystore, or a raw
// private key, depending on the configured signing method.
func resolveOperatorIdentity(ctx context.Context, logger *zap.Logger, cfg *config, res resolved) (operatorIdentity, error) {
	if cfg.ExporterOptions.Enabled {
		logger.Info("exporter mode: skipping operator signing and key manager services")
		return operatorIdentity{}, nil
	}

	if res.usingSSVSigner {
		endpointField := zap.String("ssv_signer_endpoint", cfg.SSVSigner.Endpoint)
		logger := logger.With(endpointField)
		logger.Info("using ssv-signer for signing")

		if _, err := url.ParseRequestURI(cfg.SSVSigner.Endpoint); err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("invalid ssv signer endpoint format: %w", err), fields: []zap.Field{endpointField}}
		}

		ssvSignerOptions := []ssvsigner.ClientOption{
			ssvsigner.WithLogger(logger),
			ssvsigner.WithRequestTimeout(cfg.SSVSigner.RequestTimeout),
		}

		if cfg.SSVSigner.KeystoreFile != "" || cfg.SSVSigner.ServerCertFile != "" {
			tlsConfig := &ssvsignertls.Config{
				ClientKeystoreFile:         cfg.SSVSigner.KeystoreFile,
				ClientKeystorePasswordFile: cfg.SSVSigner.KeystorePasswordFile,
				ClientServerCertFile:       cfg.SSVSigner.ServerCertFile,
			}

			clientConfig, err := tlsConfig.LoadClientTLSConfig()
			if err != nil {
				return operatorIdentity{}, startupError{err: fmt.Errorf("failed to load ssv-signer TLS config: %w", err), fields: []zap.Field{endpointField}}
			}

			ssvSignerOptions = append(ssvSignerOptions, ssvsigner.WithTLSConfig(clientConfig))
		}

		ssvSignerClient := ssvsigner.NewClient(
			cfg.SSVSigner.Endpoint,
			ssvSignerOptions...,
		)

		operatorPubKeyString, err := ssvSignerClient.OperatorIdentity(ctx)
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("ssv-signer unavailable: %w", err), fields: []zap.Field{endpointField}}
		}

		pubKeyField := zap.String(fields.FieldPubKey, operatorPubKeyString)
		logger = logger.With(pubKeyField)
		logger.Info("ssv-signer operator identity")

		operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("could not extract operator public key from string: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}

		operatorPubKeyBase64, err := operatorPubKey.Base64()
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("could not get operator public key base64: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}

		return operatorIdentity{ssvSigner: ssvSignerClient, pubKeyB64: operatorPubKeyBase64}, nil
	}

	var operatorPrivKey keys.OperatorPrivateKey
	var operatorPrivKeyPEM string
	if res.usingKeystore {
		logger.Info("getting operator private key from keystore")

		privKey, decryptedKeystore, err := privateKeyFromKeystore(cfg.KeyStore.PrivateKeyFile, cfg.KeyStore.PasswordFile)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not extract private key from keystore: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyPEM = base64.StdEncoding.EncodeToString(decryptedKeystore)
	} else if res.usingPrivKey {
		logger.Info("getting operator private key from args")

		privKey, err := keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not decode operator private key: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyPEM = cfg.OperatorPrivateKey
	}

	operatorPubKeyBase64, err := operatorPrivKey.Public().Base64()
	if err != nil {
		return operatorIdentity{}, fmt.Errorf("could not get operator public key base64: %w", err)
	}

	return operatorIdentity{privKey: operatorPrivKey, privKeyPEM: operatorPrivKeyPEM, pubKeyB64: operatorPubKeyBase64}, nil
}

// openNodeDB opens the node's database, picking the backend by mode: exporter nodes use pebble,
// operator nodes use badger.
func openNodeDB(logger *zap.Logger, cfg *config, beaconConfig *networkconfig.Beacon, operatorPrivKey keys.OperatorPrivateKey) (basedb.Database, error) {
	if cfg.ExporterOptions.Enabled {
		logger.Info("using pebble db")
		return setupPebbleDB(logger, cfg, beaconConfig, operatorPrivKey)
	}
	logger.Info("using badger db")
	return setupBadgerDB(logger, cfg, beaconConfig, operatorPrivKey)
}

// buildKeyManager constructs the operator's key manager and signer. Exporter nodes don't sign, so
// it returns (nil, nil); operator nodes get a remote key manager (ssv-signer) or a local one.
func buildKeyManager(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config,
	res resolved,
	beaconConfig *networkconfig.Beacon,
	db basedb.Database,
	ssvSignerClient *ssvsigner.Client,
	operatorPrivKey keys.OperatorPrivateKey,
	operatorDataStore operatordatastore.OperatorDataStore,
) (ekm.KeyManager, types.OperatorSigner, error) {
	if cfg.ExporterOptions.Enabled {
		return nil, nil, nil
	}

	ekmDB := ekmadapter.NewDatabaseAdapter(db)
	if res.usingSSVSigner {
		remoteKeyManager, err := ekm.NewRemoteKeyManager(
			ctx,
			logger,
			beaconConfig,
			ssvSignerClient,
			ekmDB,
			operatorDataStore.GetOperatorID,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("could not create remote key manager: %w", err)
		}

		return remoteKeyManager, remoteKeyManager, nil
	}

	localKeyManager, err := ekm.NewLocalKeyManager(logger, ekmDB, beaconConfig, operatorPrivKey)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create new eth-key-manager signer: %w", err)
	}

	return localKeyManager, types.NewSsvOperatorSigner(operatorPrivKey, operatorDataStore.GetOperatorID), nil
}
