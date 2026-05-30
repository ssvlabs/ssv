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
	"strings"
	"time"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"

	"github.com/ssvlabs/ssv/ekmadapter"

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
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/operator"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validator/metadata"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func run(ctx context.Context, cfg *config, logger *zap.Logger) error {
	// Validate the configuration up front, before any network I/O, so a misconfigured node
	// fails fast (e.g. an invalid ProposerDelay is rejected before the beacon client is built,
	// rather than after connecting to the beacon). resolveAndValidate only reads cfg.
	res, err := cfg.resolveAndValidate(logger)
	if err != nil {
		return err
	}
	usingSSVSigner, usingKeystore, usingPrivKey := res.usingSSVSigner, res.usingKeystore, res.usingPrivKey

	ssvNetworkConfig, err := setupSSVNetwork(logger)
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

	var operatorPrivKey keys.OperatorPrivateKey
	var operatorPrivKeyPEM string
	var ssvSignerClient *ssvsigner.Client
	var operatorPubKeyBase64 string

	if cfg.ExporterOptions.Enabled {
		logger.Info("exporter mode: skipping operator signing and key manager services")
	} else if usingSSVSigner {
		endpointField := zap.String("ssv_signer_endpoint", cfg.SSVSigner.Endpoint)
		logger := logger.With(endpointField)
		logger.Info("using ssv-signer for signing")

		if _, err := url.ParseRequestURI(cfg.SSVSigner.Endpoint); err != nil {
			return startupError{err: fmt.Errorf("invalid ssv signer endpoint format: %w", err), fields: []zap.Field{endpointField}}
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
				return startupError{err: fmt.Errorf("failed to load ssv-signer TLS config: %w", err), fields: []zap.Field{endpointField}}
			}

			ssvSignerOptions = append(ssvSignerOptions, ssvsigner.WithTLSConfig(clientConfig))
		}

		ssvSignerClient = ssvsigner.NewClient(
			cfg.SSVSigner.Endpoint,
			ssvSignerOptions...,
		)

		operatorPubKeyString, err := ssvSignerClient.OperatorIdentity(ctx)
		if err != nil {
			return startupError{err: fmt.Errorf("ssv-signer unavailable: %w", err), fields: []zap.Field{endpointField}}
		}

		pubKeyField := zap.String(fields.FieldPubKey, operatorPubKeyString)
		logger = logger.With(pubKeyField)
		logger.Info("ssv-signer operator identity")

		operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
		if err != nil {
			return startupError{err: fmt.Errorf("could not extract operator public key from string: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}

		operatorPubKeyBase64, err = operatorPubKey.Base64()
		if err != nil {
			return startupError{err: fmt.Errorf("could not get operator public key base64: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}
	} else {
		if usingKeystore {
			logger.Info("getting operator private key from keystore")

			var decryptedKeystore []byte
			operatorPrivKey, decryptedKeystore, err = privateKeyFromKeystore(cfg.KeyStore.PrivateKeyFile, cfg.KeyStore.PasswordFile)
			if err != nil {
				return fmt.Errorf("could not extract private key from keystore: %w", err)
			}

			operatorPrivKeyPEM = base64.StdEncoding.EncodeToString(decryptedKeystore)
		} else if usingPrivKey {
			logger.Info("getting operator private key from args")

			operatorPrivKey, err = keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
			if err != nil {
				return fmt.Errorf("could not decode operator private key: %w", err)
			}

			operatorPrivKeyPEM = cfg.OperatorPrivateKey
		}

		operatorPubKeyBase64, err = operatorPrivKey.Public().Base64()
		if err != nil {
			return fmt.Errorf("could not get operator public key base64: %w", err)
		}
	}

	cfg.DBOptions.Ctx = ctx
	var db basedb.Database
	if cfg.ExporterOptions.Enabled {
		logger.Info("using pebble db")
		db, err = setupPebbleDB(logger, networkConfig.Beacon, operatorPrivKey)
	} else {
		logger.Info("using badger db")
		db, err = setupBadgerDB(logger, networkConfig.Beacon, operatorPrivKey)
	}
	if err != nil {
		return fmt.Errorf("could not setup db: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("could not close db", zap.Error(err))
		}
	}()

	nodeStorage, err := operatorstorage.NewNodeStorage(networkConfig.Beacon, logger, db)
	if err != nil {
		return fmt.Errorf("failed to create node storage: %w", err)
	}

	if !cfg.ExporterOptions.Enabled {
		if usingSSVSigner {
			// Ensure the pubkey is saved on first run and never changes afterwards
			if err := ensureOperatorPubKey(nodeStorage, operatorPubKeyBase64); err != nil {
				return fmt.Errorf("could not save base64-encoded operator public key: %w", err)
			}
		} else {
			if err := ensureOperatorPrivateKey(nodeStorage, operatorPrivKey, operatorPrivKeyPEM); err != nil {
				return fmt.Errorf("could not save operator private key: %w", err)
			}
		}

		logger.Info("successfully loaded operator keys", zap.String(fields.FieldPubKey, operatorPubKeyBase64))
	}

	usingLocalEvents := len(cfg.LocalEventsPath) != 0

	if err := validateConfig(nodeStorage, networkConfig.StorageName(), usingLocalEvents, usingSSVSigner, cfg.ExporterOptions.Enabled); err != nil {
		return fmt.Errorf("failed to validate config: %w", err)
	}

	cfg.P2pNetworkConfig.Ctx = ctx
	operatorDataStore := setupOperatorDataStore(logger, nodeStorage, operatorPubKeyBase64)
	validatorProvider := nodeStorage.ValidatorStore().WithOperatorID(operatorDataStore.GetOperatorID)
	var validatorRegistrationSubmitter runner.ValidatorRegistrationSubmitter
	if !cfg.ExporterOptions.Enabled {
		validatorRegistrationSubmitter = runner.NewVRSubmitter(ctx, logger, networkConfig.Beacon, consensusClient, validatorProvider)
	}

	executionAddrList := strings.Split(cfg.ExecutionClient.Addr, ";")
	if len(executionAddrList) == 0 {
		return errors.New("no execution node address provided")
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

	var keyManager ekm.KeyManager
	ekmDB := ekmadapter.NewDatabaseAdapter(db)
	if !cfg.ExporterOptions.Enabled && usingSSVSigner {
		remoteKeyManager, err := ekm.NewRemoteKeyManager(
			ctx,
			logger,
			networkConfig.Beacon,
			ssvSignerClient,
			ekmDB,
			operatorDataStore.GetOperatorID,
		)
		if err != nil {
			return fmt.Errorf("could not create remote key manager: %w", err)
		}

		keyManager = remoteKeyManager
		cfg.SSVOptions.ValidatorOptions.OperatorSigner = remoteKeyManager
	} else if !cfg.ExporterOptions.Enabled {
		localKeyManager, err := ekm.NewLocalKeyManager(logger, ekmDB, networkConfig.Beacon, operatorPrivKey)
		if err != nil {
			return fmt.Errorf("could not create new eth-key-manager signer: %w", err)
		}

		keyManager = localKeyManager
		cfg.SSVOptions.ValidatorOptions.OperatorSigner = types.NewSsvOperatorSigner(operatorPrivKey, operatorDataStore.GetOperatorID)
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
	cfg.SSVOptions.ValidatorOptions.MessageValidator = messageValidator

	p2pNetwork := setupP2P(ctx, logger, db, cfg.ExporterOptions.Enabled, operatorPrivKey, ssvSignerClient)
	// The CLI owns the p2p network's lifecycle (setupP2P created it), so it also closes it
	// (operator.Node.Start no longer does). Close() is robust on partial states, so deferring
	// it right after construction is safe even if startup aborts before Setup/Start.
	defer func() {
		if err := p2pNetwork.Close(); err != nil {
			logger.Error("could not close p2p network", zap.Error(err))
		}
	}()

	cfg.SSVOptions.Context = ctx
	cfg.SSVOptions.DB = db
	cfg.SSVOptions.BeaconNode = consensusClient
	cfg.SSVOptions.ExecutionClient = executionClient
	cfg.SSVOptions.NetworkConfig = networkConfig
	cfg.SSVOptions.P2PNetwork = p2pNetwork
	cfg.SSVOptions.ValidatorOptions.NetworkConfig = networkConfig
	cfg.SSVOptions.ValidatorOptions.Context = ctx
	cfg.SSVOptions.ValidatorOptions.DB = db
	cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
	cfg.SSVOptions.ValidatorOptions.Beacon = consensusClient
	cfg.SSVOptions.ValidatorOptions.BeaconSigner = keyManager
	cfg.SSVOptions.ValidatorOptions.ValidatorsMap = validatorsMap

	cfg.SSVOptions.ValidatorOptions.OperatorDataStore = operatorDataStore
	cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
	cfg.SSVOptions.ValidatorOptions.ValidatorRegistrationSubmitter = validatorRegistrationSubmitter

	var decidedStreamPublisherFn func(dutytracer.DecidedInfo)
	if cfg.WsAPIPort != 0 {
		ws := exporterapi.NewWsServer(ctx, logger, nil, http.NewServeMux(), cfg.WithPing)
		cfg.SSVOptions.WS = ws
		cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
		cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(logger, networkConfig.DomainType, ws)
		decidedStreamPublisherFn = decided.NewDecidedListener(logger, networkConfig.DomainType, ws, nodeStorage.ValidatorStore())
	}

	cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place

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
		s := ibftstorage.New(logger, cfg.SSVOptions.ValidatorOptions.DB, storageRole)
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

	cfg.SSVOptions.ValidatorOptions.StorageMap = storageMap
	cfg.SSVOptions.ValidatorOptions.Graffiti = []byte(cfg.Graffiti)
	cfg.SSVOptions.ValidatorOptions.ProposerDelay = cfg.ProposerDelay
	cfg.SSVOptions.ValidatorOptions.ValidatorStore = nodeStorage.ValidatorStore()

	fixedSubnets, err := networkcommons.SubnetsFromString(cfg.P2pNetworkConfig.Subnets)
	if err != nil {
		logger.Fatal("failed to parse fixed subnets", zap.Error(err))
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
	cfg.SSVOptions.ValidatorOptions.ValidatorSyncer = metadataSyncer

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
		cfg.SSVOptions.ValidatorOptions.DutyTraceCollector = collector
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
	cfg.SSVOptions.ValidatorOptions.DoppelgangerHandler = doppelgangerHandler

	validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions, cfg.ExporterOptions)
	cfg.SSVOptions.ValidatorController = validatorCtrl
	cfg.SSVOptions.ValidatorStore = nodeStorage.ValidatorStore()

	operatorNode := operator.New(logger, cfg.SSVOptions, cfg.ExporterOptions, slotTickerProvider, storageMap)

	if cfg.MetricsAPIPort > 0 {
		go func() {
			metricsHandler := metrics.NewHandler(logger, db, cfg.EnableProfile, operatorNode)
			if err := metricsHandler.Start(http.NewServeMux(), fmt.Sprintf(":%d", cfg.MetricsAPIPort)); err != nil {
				logger.Fatal("failed to serve metrics", zap.Error(err))
			}
		}()
	}

	healthProber := hprobe.NewHealthProber(logger)
	healthProber.AddComponent(clComponentName, consensusClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	healthProber.AddComponent(elComponentName, executionClient, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	ensureComponentsHealthy(ctx, logger, healthProber)

	eventSyncer := syncContractEvents(
		ctx,
		logger,
		executionClient,
		validatorCtrl,
		networkConfig,
		nodeStorage,
		operatorDataStore,
		keyManager,
		doppelgangerHandler,
	)
	if len(cfg.LocalEventsPath) == 0 {
		healthProber.AddComponent(eventSyncerComponentName, eventSyncer, proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)
	}
	go startHealthProber(ctx, logger, healthProber)

	if _, err := metadataSyncer.SyncAll(ctx); err != nil {
		logger.Fatal("failed to sync metadata on startup", zap.Error(err))
	}

	if usingSSVSigner {
		ensureNoMissingKeys(ctx, logger, nodeStorage, operatorDataStore, ssvSignerClient)
	}

	// Increase MaxPeers if the operator is subscribed to many subnets.
	// TODO: use OperatorCommittees when it's fixed.
	if cfg.P2pNetworkConfig.DynamicMaxPeers {
		var (
			baseMaxPeers        = 60
			maxPeersLimit       = cfg.P2pNetworkConfig.DynamicMaxPeersLimit
			idealPeersPerSubnet = 3
		)
		start := time.Now()
		myValidators := nodeStorage.ValidatorStore().OperatorValidators(operatorDataStore.GetOperatorID())
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
		if cfg.P2pNetworkConfig.MaxPeers < idealMaxPeers {
			logger.Warn("increasing MaxPeers to match the operator's subscribed subnets",
				zap.Int("old_max_peers", cfg.P2pNetworkConfig.MaxPeers),
				zap.Int("new_max_peers", idealMaxPeers),
				zap.Int("subscribed_subnets", myActiveSubnets),
				fields.Took(time.Since(start)),
			)
			cfg.P2pNetworkConfig.MaxPeers = idealMaxPeers
		}
	}

	// Wire validator stats into pubsub peer scoring, then set up and start the p2p network.
	// These run unconditionally: gating them on DynamicMaxPeers (a long-standing bug) left
	// DynamicMaxPeers=false nodes with a network that was never Setup/Start'd — operator.Node.Start
	// then failed Subscribe* with ErrNetworkIsNotReady — and with a nil GetValidatorStats, which
	// makes pubsub peer scoring silently fall back to fake hard-coded stats. GetValidatorStats
	// must be set before Setup(), which reads it via setupPubsub.
	cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
		return validatorCtrl.GetValidatorStats()
	}
	if err := p2pNetwork.Setup(); err != nil {
		logger.Fatal("failed to setup network", zap.Error(err))
	}
	if err := p2pNetwork.Start(); err != nil {
		logger.Fatal("failed to start network", zap.Error(err))
	}
	healthProber.AddComponent(p2pComponentName, p2pNetwork.(p2pv1.HealthChecker), proberHealthcheckTimeout, proberRetriesMax, proberRetryDelay)

	if cfg.SSVAPIPort > 0 {
		warnIfSSVAPIAddressUnset(logger, cfg.SSVAPIAddress, cfg.SSVAPIPort)
		apiServer := apiserver.New(
			logger,
			net.JoinHostPort(cfg.SSVAPIAddress, strconv.Itoa(cfg.SSVAPIPort)),
			hnode.NewNode(
				// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
				[]string{fmt.Sprintf("tcp://%s:%d", cfg.P2pNetworkConfig.HostAddress, cfg.P2pNetworkConfig.TCPPort), fmt.Sprintf("udp://%s:%d", cfg.P2pNetworkConfig.HostAddress, cfg.P2pNetworkConfig.UDPPort)},
				p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
				p2pNetwork.(p2pv1.HostProvider).Host().Network(),
				p2pNetwork,
				healthProber,
				clComponentName,
				elComponentName,
				eventSyncerComponentName,
			),
			&hvalidators.Validators{
				Shares: nodeStorage.Shares(),
			},
			hexporter.NewExporter(logger, storageMap, collector, nodeStorage.ValidatorStore()),
			res.mode == modeExporterArchive,
		)
		go func() {
			err := apiServer.Run()
			if err != nil {
				logger.Fatal("failed to start API server", zap.Error(err))
			}
		}()
	}
	if err := operatorNode.Start(cfg.SSVOptions.Context); err != nil {
		logger.Fatal("failed to start SSV node", zap.Error(err))
	}

	return nil
}
