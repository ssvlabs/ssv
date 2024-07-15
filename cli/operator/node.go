package operator

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/ssvlabs/ssv/exporter/convert"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api/handlers"
	apiserver "github.com/ssvlabs/ssv/api/server"
	"github.com/ssvlabs/ssv/beacon/goclient"
	global_config "github.com/ssvlabs/ssv/cli/config"
	"github.com/ssvlabs/ssv/ekm"
	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/eventsyncer"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/localevents"
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/exporter/api/decided"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	ssv_identity "github.com/ssvlabs/ssv/identity"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	genesisvalidation "github.com/ssvlabs/ssv/message/validation/genesis"
	"github.com/ssvlabs/ssv/migrations"
	"github.com/ssvlabs/ssv/monitoring/metrics"
	"github.com/ssvlabs/ssv/monitoring/metricsreporter"
	"github.com/ssvlabs/ssv/network"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/nodeprobe"
	"github.com/ssvlabs/ssv/operator"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validators"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/commons"
	"github.com/ssvlabs/ssv/utils/format"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
)

type KeyStore struct {
	PrivateKeyFile string `yaml:"PrivateKeyFile" env:"PRIVATE_KEY_FILE" env-description:"Operator private key file"`
	PasswordFile   string `yaml:"PasswordFile" env:"PASSWORD_FILE" env-description:"Password for operator private key file decryption"`
}

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options                   `yaml:"db"`
	SSVOptions                 operator.Options                 `yaml:"ssv"`
	ExecutionClient            executionclient.ExecutionOptions `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient            beaconprotocol.Options           `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig           p2pv1.Config                     `yaml:"p2p"`
	KeyStore                   KeyStore                         `yaml:"KeyStore"`
	OperatorPrivateKey         string                           `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	MetricsAPIPort             int                              `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"Port to listen on for the metrics API."`
	EnableProfile              bool                             `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string                           `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`
	WsAPIPort                  int                              `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"Port to listen on for the websocket API."`
	WithPing                   bool                             `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`
	SSVAPIPort                 int                              `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port to listen on for the SSV API."`
	LocalEventsPath            string                           `yaml:"LocalEventsPath" env:"EVENTS_PATH" env-description:"path to local events"`
}

var cfg config

var globalArgs global_config.Args

var operatorNode operator.Node

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)

		logger, err := setupGlobal()
		if err != nil {
			log.Fatal("could not create logger", err)
		}

		defer logging.CapturePanic(logger)

		logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

		metricsReporter := metricsreporter.New(
			metricsreporter.WithLogger(logger),
		)

		networkConfig, err := setupSSVNetwork(logger)
		if err != nil {
			logger.Fatal("could not setup network", zap.Error(err))
		}
		cfg.DBOptions.Ctx = cmd.Context()
		db, err := setupDB(logger, networkConfig.Beacon.GetNetwork())
		if err != nil {
			logger.Fatal("could not setup db", zap.Error(err))
		}

		var operatorPrivKey keys.OperatorPrivateKey
		var operatorPrivKeyText string
		if cfg.KeyStore.PrivateKeyFile != "" {
			// nolint: gosec
			encryptedJSON, err := os.ReadFile(cfg.KeyStore.PrivateKeyFile)
			if err != nil {
				logger.Fatal("could not read PEM file", zap.Error(err))
			}

			// nolint: gosec
			keyStorePassword, err := os.ReadFile(cfg.KeyStore.PasswordFile)
			if err != nil {
				logger.Fatal("could not read password file", zap.Error(err))
			}

			decryptedKeystore, err := keystore.DecryptKeystore(encryptedJSON, string(keyStorePassword))
			if err != nil {
				logger.Fatal("could not decrypt operator private key keystore", zap.Error(err))
			}
			operatorPrivKey, err = keys.PrivateKeyFromBytes(decryptedKeystore)
			if err != nil {
				logger.Fatal("could not extract operator private key from file", zap.Error(err))
			}

			operatorPrivKeyText = base64.StdEncoding.EncodeToString(decryptedKeystore)
		} else {
			operatorPrivKey, err = keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
			if err != nil {
				logger.Fatal("could not decode operator private key", zap.Error(err))
			}
			operatorPrivKeyText = cfg.OperatorPrivateKey
		}
		cfg.P2pNetworkConfig.OperatorSigner = operatorPrivKey

		nodeStorage, operatorData := setupOperatorStorage(logger, db, operatorPrivKey, operatorPrivKeyText)
		operatorDataStore := operatordatastore.New(operatorData)

		usingLocalEvents := len(cfg.LocalEventsPath) != 0

		verifyConfig(logger, nodeStorage, networkConfig.Name, usingLocalEvents)

		ekmHashedKey, err := operatorPrivKey.EKMHash()
		if err != nil {
			logger.Fatal("could not get operator private key hash", zap.Error(err))
		}

		keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, ekmHashedKey)
		if err != nil {
			logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
		}

		cfg.P2pNetworkConfig.Ctx = cmd.Context()

		slotTickerProvider := func() slotticker.SlotTicker {
			return slotticker.New(logger, slotticker.Config{
				SlotDuration: networkConfig.SlotDurationSec(),
				GenesisTime:  networkConfig.GetGenesisTime(),
			})
		}

		cfg.ConsensusClient.Context = cmd.Context()
		cfg.ConsensusClient.Graffiti = []byte("SSV.Network")
		cfg.ConsensusClient.GasLimit = spectypes.DefaultGasLimit
		cfg.ConsensusClient.Network = networkConfig.Beacon.GetNetwork()

		consensusClient := setupConsensusClient(logger, operatorDataStore, slotTickerProvider)

		executionClient, err := executionclient.New(
			cmd.Context(),
			cfg.ExecutionClient.Addr,
			ethcommon.HexToAddress(networkConfig.RegistryContractAddr),
			executionclient.WithLogger(logger),
			executionclient.WithMetrics(metricsReporter),
			executionclient.WithFollowDistance(executionclient.DefaultFollowDistance),
			executionclient.WithConnectionTimeout(cfg.ExecutionClient.ConnectionTimeout),
			executionclient.WithReconnectionInitialInterval(executionclient.DefaultReconnectionInitialInterval),
			executionclient.WithReconnectionMaxInterval(executionclient.DefaultReconnectionMaxInterval),
		)
		if err != nil {
			logger.Fatal("could not connect to execution client", zap.Error(err))
		}

		cfg.P2pNetworkConfig.NodeStorage = nodeStorage
		cfg.P2pNetworkConfig.OperatorPubKeyHash = format.OperatorID(operatorData.PublicKey)
		cfg.P2pNetworkConfig.OperatorDataStore = operatorDataStore
		cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode
		cfg.P2pNetworkConfig.Network = networkConfig

		validatorsMap := validators.New(cmd.Context())

		dutyStore := dutystore.New()
		cfg.SSVOptions.DutyStore = dutyStore

		signatureVerifier := signatureverifier.NewSignatureVerifier(nodeStorage)

		validatorStore := nodeStorage.ValidatorStore()
		// validatorStore = newValidatorStore(...) // TODO

		var messageValidator validation.MessageValidator

		if networkConfig.AlanFork() {
			messageValidator = validation.New(
				networkConfig,
				validatorStore,
				dutyStore,
				signatureVerifier,
				validation.WithLogger(logger),
				validation.WithMetrics(metricsReporter),
			)
		} else {
			messageValidator = genesisvalidation.New(
				networkConfig,
				genesisvalidation.WithNodeStorage(nodeStorage),
				genesisvalidation.WithLogger(logger),
				genesisvalidation.WithMetrics(metricsReporter),
				genesisvalidation.WithDutyStore(dutyStore),
			)
		}

		cfg.P2pNetworkConfig.Metrics = metricsReporter
		cfg.P2pNetworkConfig.MessageValidator = messageValidator
		cfg.SSVOptions.ValidatorOptions.MessageValidator = messageValidator

		p2pNetwork := setupP2P(logger, db, metricsReporter)

		cfg.SSVOptions.Context = cmd.Context()
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.BeaconNode = consensusClient
		cfg.SSVOptions.ExecutionClient = executionClient
		cfg.SSVOptions.Network = networkConfig
		cfg.SSVOptions.P2PNetwork = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.NetworkConfig = networkConfig
		cfg.SSVOptions.ValidatorOptions.BeaconNetwork = networkConfig.Beacon.GetNetwork()
		cfg.SSVOptions.ValidatorOptions.Context = cmd.Context()
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.Beacon = consensusClient
		cfg.SSVOptions.ValidatorOptions.BeaconSigner = keyManager
		cfg.SSVOptions.ValidatorOptions.ValidatorsMap = validatorsMap
		cfg.SSVOptions.ValidatorOptions.NetworkConfig = networkConfig

		cfg.SSVOptions.ValidatorOptions.OperatorDataStore = operatorDataStore
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
		cfg.SSVOptions.ValidatorOptions.RecipientsStorage = nodeStorage
		cfg.SSVOptions.ValidatorOptions.GasLimit = cfg.ConsensusClient.GasLimit

		genesisKeyManager := &ekm.GenesisKeyManagerAdapter{KeyManager: keyManager}
		cfg.SSVOptions.ValidatorOptions.GenesisControllerOptions.KeyManager = genesisKeyManager

		if cfg.WsAPIPort != 0 {
			ws := exporterapi.NewWsServer(cmd.Context(), nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(logger, ws)
		}

		cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place

		storageRoles := []convert.RunnerRole{
			convert.RoleCommittee,
			convert.RoleAttester,
			convert.RoleProposer,
			convert.RoleSyncCommittee,
			convert.RoleAggregator,
			convert.RoleSyncCommitteeContribution,
			convert.RoleValidatorRegistration,
			convert.RoleVoluntaryExit,
		}

		storageMap := ibftstorage.NewStores()

		for _, storageRole := range storageRoles {
			storageMap.Add(storageRole, ibftstorage.New(cfg.SSVOptions.ValidatorOptions.DB, storageRole.String()))
		}

		cfg.SSVOptions.ValidatorOptions.StorageMap = storageMap
		cfg.SSVOptions.ValidatorOptions.Metrics = metricsReporter
		cfg.SSVOptions.ValidatorOptions.ValidatorStore = nodeStorage.ValidatorStore()
		cfg.SSVOptions.ValidatorOptions.OperatorSigner = types.NewSsvOperatorSigner(operatorPrivKey, operatorDataStore.GetOperatorID)
		cfg.SSVOptions.Metrics = metricsReporter

		validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl
		cfg.SSVOptions.ValidatorStore = validatorStore

		operatorNode = operator.New(logger, cfg.SSVOptions, slotTickerProvider, storageMap)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), logger, db, metricsReporter, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		nodeProber := nodeprobe.NewProber(
			logger,
			func() {
				logger.Fatal("ethereum node(s) are either out of sync or down. Ensure the nodes are healthy to resume.")
			},
			map[string]nodeprobe.Node{
				"execution client": executionClient,

				// Underlying options.Beacon's value implements nodeprobe.StatusChecker.
				// However, as it uses spec's specssv.BeaconNode interface, avoiding type assertion requires modifications in spec.
				// If options.Beacon doesn't implement nodeprobe.StatusChecker due to a mistake, this would panic early.
				"consensus client": consensusClient.(nodeprobe.Node),
			},
		)

		nodeProber.Start(cmd.Context())
		nodeProber.Wait()
		logger.Info("ethereum node(s) are healthy")

		metricsReporter.SSVNodeHealthy()

		eventSyncer := setupEventHandling(
			cmd.Context(),
			logger,
			executionClient,
			validatorCtrl,
			storageMap,
			metricsReporter,
			networkConfig,
			nodeStorage,
			operatorDataStore,
			operatorPrivKey,
			keyManager,
		)
		nodeProber.AddNode("event syncer", eventSyncer)

		cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return validatorCtrl.GetValidatorStats()
		}
		if err := p2pNetwork.Setup(logger); err != nil {
			logger.Fatal("failed to setup network", zap.Error(err))
		}
		if err := p2pNetwork.Start(logger); err != nil {
			logger.Fatal("failed to start network", zap.Error(err))
		}

		if cfg.SSVAPIPort > 0 {
			apiServer := apiserver.New(
				logger,
				fmt.Sprintf(":%d", cfg.SSVAPIPort),
				&handlers.Node{
					// TODO: replace with narrower interface! (instead of accessing the entire PeersIndex)
					ListenAddresses: []string{fmt.Sprintf("tcp://%s:%d", cfg.P2pNetworkConfig.HostAddress, cfg.P2pNetworkConfig.TCPPort), fmt.Sprintf("udp://%s:%d", cfg.P2pNetworkConfig.HostAddress, cfg.P2pNetworkConfig.UDPPort)},
					PeersIndex:      p2pNetwork.(p2pv1.PeersIndexProvider).PeersIndex(),
					Network:         p2pNetwork.(p2pv1.HostProvider).Host().Network(),
					TopicIndex:      p2pNetwork.(handlers.TopicIndex),
					NodeProber:      nodeProber,
				},
				&handlers.Validators{
					Shares: nodeStorage.Shares(),
				},
			)
			go func() {
				err := apiServer.Run()
				if err != nil {
					logger.Fatal("failed to start API server", zap.Error(err))
				}
			}()
		}
		if err := operatorNode.Start(logger); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func verifyConfig(logger *zap.Logger, nodeStorage operatorstorage.Storage, networkName string, usingLocalEvents bool) {
	storedConfig, foundConfig, err := nodeStorage.GetConfig(nil)
	if err != nil {
		logger.Fatal("could not check saved local events config", zap.Error(err))
	}

	currentConfig := &operatorstorage.ConfigLock{
		NetworkName:      networkName,
		UsingLocalEvents: usingLocalEvents,
	}

	if foundConfig {
		if err := storedConfig.EnsureSameWith(currentConfig); err != nil {
			err = fmt.Errorf("incompatible config change: %w", err)
			logger.Fatal(err.Error())
		}
	} else {
		if err := nodeStorage.SaveConfig(nil, currentConfig); err != nil {
			err = fmt.Errorf("failed to store config: %w", err)
			logger.Fatal(err.Error())
		}
	}
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func setupGlobal() (*zap.Logger, error) {
	if globalArgs.ConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read config: %w", err)
		}
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read share config: %w", err)
		}
	}

	err := logging.SetGlobalLogger(
		cfg.LogLevel,
		cfg.LogLevelFormat,
		cfg.LogFormat,
		&logging.LogFileOptions{
			FileName:   cfg.LogFilePath,
			MaxSize:    cfg.LogFileSize,
			MaxBackups: cfg.LogFileBackups,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("logging.SetGlobalLogger: %w", err)
	}

	return zap.L(), nil
}

func setupDB(logger *zap.Logger, eth2Network beaconprotocol.Network) (*kv.BadgerDB, error) {
	db, err := kv.New(logger, cfg.DBOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open db")
	}
	reopenDb := func() error {
		if err := db.Close(); err != nil {
			return errors.Wrap(err, "failed to close db")
		}
		db, err = kv.New(logger, cfg.DBOptions)
		return errors.Wrap(err, "failed to reopen db")
	}

	migrationOpts := migrations.Options{
		Db:      db,
		DbPath:  cfg.DBOptions.Path,
		Network: eth2Network,
	}
	applied, err := migrations.Run(cfg.DBOptions.Ctx, logger, migrationOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to run migrations")
	}
	if applied == 0 {
		return db, nil
	}

	// If migrations were applied, we run a full garbage collection cycle
	// to reclaim any space that may have been freed up.
	// Close & reopen the database to trigger any unknown internal
	// startup/shutdown procedures that the storage engine may have.
	start := time.Now()
	if err := reopenDb(); err != nil {
		return nil, err
	}

	// Run a long garbage collection cycle with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := db.FullGC(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to collect garbage")
	}

	// Close & reopen again.
	if err := reopenDb(); err != nil {
		return nil, err
	}
	logger.Debug("post-migrations garbage collection completed", fields.Duration(start))

	return db, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.Database, configPrivKey keys.OperatorPrivateKey, configPrivKeyText string) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		logger.Fatal("failed to create node storage", zap.Error(err))
	}

	storedPrivKeyHash, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil {
		logger.Fatal("could not get hashed private key", zap.Error(err))
	}

	configStoragePrivKeyHash, err := configPrivKey.StorageHash()
	if err != nil {
		logger.Fatal("could not hash private key", zap.Error(err))
	}

	// Backwards compatibility for the old hashing method,
	// which was hashing the text from the configuration directly,
	// whereas StorageHash re-encodes with PEM format.
	cliPrivKeyDecoded, err := base64.StdEncoding.DecodeString(configPrivKeyText)
	if err != nil {
		logger.Fatal("could not decode private key", zap.Error(err))
	}
	configStoragePrivKeyLegacyHash, err := rsaencryption.HashRsaKey(cliPrivKeyDecoded)
	if err != nil {
		logger.Fatal("could not hash private key", zap.Error(err))
	}

	if !found {
		if err := nodeStorage.SavePrivateKeyHash(configStoragePrivKeyHash); err != nil {
			logger.Fatal("could not save hashed private key", zap.Error(err))
		}
	} else if configStoragePrivKeyHash != storedPrivKeyHash &&
		configStoragePrivKeyLegacyHash != storedPrivKeyHash {
		logger.Fatal("operator private key is not matching the one encrypted the storage")
	}

	encodedPubKey, err := configPrivKey.Public().Base64()
	if err != nil {
		logger.Fatal("could not encode public key", zap.Error(err))
	}

	logger.Info("successfully loaded operator keys", zap.String("pubkey", string(encodedPubKey)))

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, encodedPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: encodedPubKey,
		}
	}
	if operatorData == nil {
		logger.Fatal("invalid operator data in database: nil")
	}

	return nodeStorage, operatorData
}

func setupSSVNetwork(logger *zap.Logger) (networkconfig.NetworkConfig, error) {
	networkConfig, err := networkconfig.GetNetworkConfigByName(cfg.SSVOptions.NetworkName)
	if err != nil {
		return networkconfig.NetworkConfig{}, err
	}

	nodeType := "light"
	if cfg.SSVOptions.ValidatorOptions.FullNode {
		nodeType = "full"
	}

	logger.Info("setting ssv network",
		fields.Network(networkConfig.Name),
		fields.Domain(networkConfig.Domain),
		zap.String("nodeType", nodeType),
		zap.Any("beaconNetwork", networkConfig.Beacon.GetNetwork().BeaconNetwork),
		zap.Uint64("genesisEpoch", uint64(networkConfig.GenesisEpoch)),
		zap.String("registryContract", networkConfig.RegistryContractAddr),
	)

	return networkConfig, nil
}

func setupP2P(logger *zap.Logger, db basedb.Database, mr metricsreporter.MetricsReporter) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db)
	netPrivKey, err := istore.SetupNetworkKey(logger, cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}
	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey

	return p2pv1.New(logger, &cfg.P2pNetworkConfig, mr)
}

func setupConsensusClient(
	logger *zap.Logger,
	operatorDataStore operatordatastore.OperatorDataStore,
	slotTickerProvider slotticker.Provider,
) beaconprotocol.BeaconNode {
	cl, err := goclient.New(logger, cfg.ConsensusClient, operatorDataStore, slotTickerProvider)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			fields.Address(cfg.ConsensusClient.BeaconNodeAddr))
	}

	return cl
}

func setupEventHandling(
	ctx context.Context,
	logger *zap.Logger,
	executionClient *executionclient.ExecutionClient,
	validatorCtrl validator.Controller,
	storageMap *ibftstorage.QBFTStores,
	metricsReporter metricsreporter.MetricsReporter,
	networkConfig networkconfig.NetworkConfig,
	nodeStorage operatorstorage.Storage,
	operatorDataStore operatordatastore.OperatorDataStore,
	operatorDecrypter keys.OperatorDecrypter,
	keyManager ekm.KeyManager,
) *eventsyncer.EventSyncer {
	eventFilterer, err := executionClient.Filterer()
	if err != nil {
		logger.Fatal("failed to set up event filterer", zap.Error(err))
	}

	eventParser := eventparser.New(eventFilterer)

	eventHandler, err := eventhandler.New(
		nodeStorage,
		eventParser,
		validatorCtrl,
		networkConfig,
		operatorDataStore,
		operatorDecrypter,
		keyManager,
		cfg.SSVOptions.ValidatorOptions.Beacon,
		storageMap,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
		eventhandler.WithMetrics(metricsReporter),
	)
	if err != nil {
		logger.Fatal("failed to setup event data handler", zap.Error(err))
	}

	eventSyncer := eventsyncer.New(
		nodeStorage,
		executionClient,
		eventHandler,
		eventsyncer.WithLogger(logger),
		eventsyncer.WithMetrics(metricsReporter),
	)

	fromBlock, found, err := nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		logger.Fatal("syncing registry contract events failed, could not get last processed block", zap.Error(err))
	}
	if !found {
		fromBlock = networkConfig.RegistrySyncOffset
	} else if fromBlock == nil {
		logger.Fatal("syncing registry contract events failed, last processed block is nil")
	} else {
		// Start syncing from the next block.
		fromBlock = new(big.Int).SetUint64(fromBlock.Uint64() + 1)
	}

	// load & parse local events yaml if exists, otherwise sync from contract
	if len(cfg.LocalEventsPath) != 0 {
		localEvents, err := localevents.Load(cfg.LocalEventsPath)
		if err != nil {
			logger.Fatal("failed to load local events", zap.Error(err))
		}

		if err := eventHandler.HandleLocalEvents(localEvents); err != nil {
			logger.Fatal("error occurred while running event data handler", zap.Error(err))
		}
	} else {
		// Sync historical registry events.
		logger.Debug("syncing historical registry events", zap.Uint64("fromBlock", fromBlock.Uint64()))
		lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, fromBlock.Uint64())
		switch {
		case errors.Is(err, executionclient.ErrNothingToSync):
			// Nothing was synced, keep fromBlock as is.
		case err == nil:
			// Advance fromBlock to the block after lastProcessedBlock.
			fromBlock = new(big.Int).SetUint64(lastProcessedBlock + 1)
		default:
			logger.Fatal("failed to sync historical registry events", zap.Error(err))
		}

		// Print registry stats.
		shares := nodeStorage.Shares().List(nil)
		operators, err := nodeStorage.ListOperators(nil, 0, 0)
		if err != nil {
			logger.Error("failed to get operators", zap.Error(err))
		}

		operatorValidators := 0
		liquidatedValidators := 0
		operatorID := operatorDataStore.GetOperatorID()
		if operatorDataStore.OperatorIDReady() {
			for _, share := range shares {
				if share.BelongsToOperator(operatorID) {
					operatorValidators++
				}
				if share.Liquidated {
					liquidatedValidators++
				}
			}
		}
		logger.Info("historical registry sync stats",
			zap.Uint64("my_operator_id", operatorID),
			zap.Int("operators", len(operators)),
			zap.Int("validators", len(shares)),
			zap.Int("liquidated_validators", liquidatedValidators),
			zap.Int("my_validators", operatorValidators),
		)

		// Sync ongoing registry events in the background.
		go func() {
			err = eventSyncer.SyncOngoing(ctx, fromBlock.Uint64())

			// Crash if ongoing sync has stopped, regardless of the reason.
			logger.Fatal("failed syncing ongoing registry events",
				zap.Uint64("last_processed_block", lastProcessedBlock),
				zap.Error(err))
		}()
	}

	return eventSyncer
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, db basedb.Database, metricsReporter metricsreporter.MetricsReporter, port int, enableProf bool) {
	logger = logger.Named(logging.NameMetricsHandler)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, db, metricsReporter, enableProf, operatorNode.(metrics.HealthChecker))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(logger, http.NewServeMux(), addr); err != nil {
		logger.Panic("failed to serve metrics", zap.Error(err))
	}
}
