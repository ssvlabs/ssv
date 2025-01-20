package operator

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	"github.com/ssvlabs/ssv/migrations"
	"github.com/ssvlabs/ssv/monitoring/metrics"
	"github.com/ssvlabs/ssv/network"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/nodeprobe"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/operator"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/operator/validator/metadata"
	"github.com/ssvlabs/ssv/operator/validators"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
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
	Graffiti                   string                           `yaml:"Graffiti" env:"GRAFFITI" env-description:"Custom graffiti for block proposals." env-default:"ssv.network" `
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

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)

		logger, err := setupGlobal()
		if err != nil {
			log.Fatal("could not create logger ", err)
		}

		defer logging.CapturePanic(logger)

		logger.Info(fmt.Sprintf("starting %v", commons.GetBuildData()))

		observabilityShutdown, err := observability.Initialize(
			cmd.Parent().Short,
			cmd.Parent().Version,
			observability.WithMetrics())
		if err != nil {
			logger.Fatal("could not initialize observability configuration", zap.Error(err))
		}

		defer func() {
			if err = observabilityShutdown(cmd.Context()); err != nil {
				logger.Error("could not shutdown observability object", zap.Error(err))
			}
		}()

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

		if err := validateConfig(nodeStorage, networkConfig.NetworkName(), usingLocalEvents); err != nil {
			logger.Fatal("failed to validate config", zap.Error(err))
		}

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
		cfg.ConsensusClient.Network = networkConfig.Beacon.GetNetwork()

		consensusClient := setupConsensusClient(logger, operatorDataStore, slotTickerProvider)

		executionAddrList := strings.Split(cfg.ExecutionClient.Addr, ";") // TODO: Decide what symbol to use as a separator. Bootnodes are currently separated by ";". Deployment bot currently uses ",".
		if len(executionAddrList) == 0 {
			logger.Fatal("no execution node address provided")
		}

		var executionClient executionclient.Provider

		if len(executionAddrList) == 1 {
			ec, err := executionclient.New(
				cmd.Context(),
				executionAddrList[0],
				ethcommon.HexToAddress(networkConfig.RegistryContractAddr),
				executionclient.WithLogger(logger),
				executionclient.WithFollowDistance(executionclient.DefaultFollowDistance),
				executionclient.WithConnectionTimeout(cfg.ExecutionClient.ConnectionTimeout),
				executionclient.WithReconnectionInitialInterval(executionclient.DefaultReconnectionInitialInterval),
				executionclient.WithReconnectionMaxInterval(executionclient.DefaultReconnectionMaxInterval),
				executionclient.WithHealthInvalidationInterval(executionclient.DefaultHealthInvalidationInterval),
				executionclient.WithSyncDistanceTolerance(cfg.ExecutionClient.SyncDistanceTolerance),
			)
			if err != nil {
				logger.Fatal("could not connect to execution client", zap.Error(err))
			}

			executionClient = ec
		} else {
			ec, err := executionclient.NewMulti(
				cmd.Context(),
				executionAddrList,
				ethcommon.HexToAddress(networkConfig.RegistryContractAddr),
				executionclient.WithLoggerMulti(logger),
				executionclient.WithFollowDistanceMulti(executionclient.DefaultFollowDistance),
				executionclient.WithConnectionTimeoutMulti(cfg.ExecutionClient.ConnectionTimeout),
				executionclient.WithReconnectionInitialIntervalMulti(executionclient.DefaultReconnectionInitialInterval),
				executionclient.WithReconnectionMaxIntervalMulti(executionclient.DefaultReconnectionMaxInterval),
				executionclient.WithHealthInvalidationIntervalMulti(executionclient.DefaultHealthInvalidationInterval),
				executionclient.WithSyncDistanceToleranceMulti(cfg.ExecutionClient.SyncDistanceTolerance),
			)
			if err != nil {
				logger.Fatal("could not connect to execution client", zap.Error(err))
			}

			executionClient = ec
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

		messageValidator := validation.New(
			networkConfig,
			nodeStorage.ValidatorStore(),
			dutyStore,
			signatureVerifier,
			validation.WithLogger(logger),
		)

		cfg.P2pNetworkConfig.MessageValidator = messageValidator
		cfg.SSVOptions.ValidatorOptions.MessageValidator = messageValidator

		p2pNetwork := setupP2P(logger, db)

		cfg.SSVOptions.Context = cmd.Context()
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.BeaconNode = consensusClient
		cfg.SSVOptions.ExecutionClient = executionClient
		cfg.SSVOptions.Network = networkConfig
		cfg.SSVOptions.P2PNetwork = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.NetworkConfig = networkConfig
		cfg.SSVOptions.ValidatorOptions.Context = cmd.Context()
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.Beacon = consensusClient
		cfg.SSVOptions.ValidatorOptions.BeaconSigner = keyManager
		cfg.SSVOptions.ValidatorOptions.ValidatorsMap = validatorsMap

		cfg.SSVOptions.ValidatorOptions.OperatorDataStore = operatorDataStore
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage
		cfg.SSVOptions.ValidatorOptions.RecipientsStorage = nodeStorage

		if cfg.WsAPIPort != 0 {
			ws := exporterapi.NewWsServer(cmd.Context(), nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(networkConfig, logger, ws)
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
			s := ibftstorage.New(cfg.SSVOptions.ValidatorOptions.DB, storageRole)
			storageMap.Add(storageRole, s)
		}

		if cfg.SSVOptions.ValidatorOptions.Exporter {
			retain := cfg.SSVOptions.ValidatorOptions.ExporterRetainSlots
			threshold := cfg.SSVOptions.Network.Beacon.EstimatedCurrentSlot()
			initSlotPruning(cmd.Context(), logger, storageMap, slotTickerProvider, threshold, retain)
		}

		cfg.SSVOptions.ValidatorOptions.StorageMap = storageMap
		cfg.SSVOptions.ValidatorOptions.Graffiti = []byte(cfg.Graffiti)
		cfg.SSVOptions.ValidatorOptions.ValidatorStore = nodeStorage.ValidatorStore()
		cfg.SSVOptions.ValidatorOptions.OperatorSigner = types.NewSsvOperatorSigner(operatorPrivKey, operatorDataStore.GetOperatorID)

		metadataSyncer := metadata.NewSyncer(
			logger,
			nodeStorage.Shares(),
			nodeStorage.ValidatorStore().WithOperatorID(operatorDataStore.GetOperatorID),
			networkConfig.Beacon,
			consensusClient,
			p2pNetwork.FixedSubnets(),
			metadata.WithSyncInterval(cfg.SSVOptions.ValidatorOptions.MetadataUpdateInterval),
		)
		cfg.SSVOptions.ValidatorOptions.ValidatorSyncer = metadataSyncer

		validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl
		cfg.SSVOptions.ValidatorStore = nodeStorage.ValidatorStore()

		operatorNode := operator.New(logger, cfg.SSVOptions, slotTickerProvider, storageMap)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(logger, db, cfg.MetricsAPIPort, cfg.EnableProfile, operatorNode)
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
				"consensus client": consensusClient,
			},
		)

		nodeProber.Start(cmd.Context())
		nodeProber.Wait()
		logger.Info("ethereum node(s) are healthy")

		eventSyncer := syncContractEvents(
			cmd.Context(),
			logger,
			executionClient,
			validatorCtrl,
			networkConfig,
			nodeStorage,
			operatorDataStore,
			operatorPrivKey,
			keyManager,
		)
		if len(cfg.LocalEventsPath) == 0 {
			nodeProber.AddNode("event syncer", eventSyncer)
		}

		if _, err := metadataSyncer.SyncOnStartup(cmd.Context()); err != nil {
			logger.Fatal("failed to sync metadata on startup", zap.Error(err))
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
			myValidators := nodeStorage.ValidatorStore().OperatorValidators(operatorData.ID)
			mySubnets := make(records.Subnets, networkcommons.SubnetsCount)
			myActiveSubnets := 0
			for _, v := range myValidators {
				subnet := networkcommons.CommitteeSubnet(v.CommitteeID())
				if mySubnets[subnet] == 0 {
					mySubnets[subnet] = 1
					myActiveSubnets++
				}
			}
			idealMaxPeers := min(baseMaxPeers+idealPeersPerSubnet*myActiveSubnets, maxPeersLimit)
			if cfg.P2pNetworkConfig.MaxPeers < idealMaxPeers {
				logger.Warn("increasing MaxPeers to match the operator's subscribed subnets",
					zap.Int("old_max_peers", cfg.P2pNetworkConfig.MaxPeers),
					zap.Int("new_max_peers", idealMaxPeers),
					zap.Int("subscribed_subnets", myActiveSubnets),
					zap.Duration("took", time.Since(start)),
				)
				cfg.P2pNetworkConfig.MaxPeers = idealMaxPeers
			}

			cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
				return validatorCtrl.GetValidatorStats()
			}
			if err := p2pNetwork.Setup(logger); err != nil {
				logger.Fatal("failed to setup network", zap.Error(err))
			}
			if err := p2pNetwork.Start(logger); err != nil {
				logger.Fatal("failed to start network", zap.Error(err))
			}
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
				&handlers.Exporter{
					NetworkConfig:     networkConfig,
					ParticipantStores: storageMap,
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

func validateConfig(nodeStorage operatorstorage.Storage, networkName string, usingLocalEvents bool) error {
	storedConfig, foundConfig, err := nodeStorage.GetConfig(nil)
	if err != nil {
		return fmt.Errorf("failed to get stored config: %w", err)
	}

	currentConfig := &operatorstorage.ConfigLock{
		NetworkName:      networkName,
		UsingLocalEvents: usingLocalEvents,
	}

	if foundConfig {
		if err := storedConfig.ValidateCompatibility(currentConfig); err != nil {
			return fmt.Errorf("incompatible config change: %w", err)
		}
	} else {

		if err := nodeStorage.SaveConfig(nil, currentConfig); err != nil {
			return fmt.Errorf("failed to store config: %w", err)
		}
	}

	return nil
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

	if cfg.SSVOptions.CustomDomainType != "" {
		if !strings.HasPrefix(cfg.SSVOptions.CustomDomainType, "0x") {
			return networkconfig.NetworkConfig{}, errors.New("custom domain type must be a hex string")
		}
		domainBytes, err := hex.DecodeString(cfg.SSVOptions.CustomDomainType[2:])
		if err != nil {
			return networkconfig.NetworkConfig{}, errors.Wrap(err, "failed to decode custom domain type")
		}
		if len(domainBytes) != 4 {
			return networkconfig.NetworkConfig{}, errors.New("custom domain type must be 4 bytes")
		}

		// https://github.com/ssvlabs/ssv/pull/1808 incremented the post-fork domain type by 1, so we have to maintain the compatibility.
		postForkDomain := binary.BigEndian.Uint32(domainBytes) + 1
		binary.BigEndian.PutUint32(networkConfig.DomainType[:], postForkDomain)

		logger.Info("running with custom domain type",
			fields.Domain(networkConfig.DomainType),
		)
	}

	nodeType := "light"
	if cfg.SSVOptions.ValidatorOptions.FullNode {
		nodeType = "full"
	}

	logger.Info("setting ssv network",
		fields.Network(networkConfig.Name),
		fields.Domain(networkConfig.DomainType),
		zap.String("nodeType", nodeType),
		zap.Any("beaconNetwork", networkConfig.Beacon.GetNetwork().BeaconNetwork),
		zap.Uint64("genesisEpoch", uint64(networkConfig.GenesisEpoch)),
		zap.String("registryContract", networkConfig.RegistryContractAddr),
	)

	return networkConfig, nil
}

func setupP2P(logger *zap.Logger, db basedb.Database) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db)
	netPrivKey, err := istore.SetupNetworkKey(logger, cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}
	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey

	n, err := p2pv1.New(logger, &cfg.P2pNetworkConfig)
	if err != nil {
		logger.Fatal("failed to setup p2p network", zap.Error(err))
	}
	return n
}

func setupConsensusClient(
	logger *zap.Logger,
	operatorDataStore operatordatastore.OperatorDataStore,
	slotTickerProvider slotticker.Provider,
) *goclient.GoClient {
	cl, err := goclient.New(logger, cfg.ConsensusClient, operatorDataStore, slotTickerProvider)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			fields.Address(cfg.ConsensusClient.BeaconNodeAddr))
	}

	return cl
}

// syncContractEvents blocks until historical events are synced and then spawns a goroutine syncing ongoing events.
func syncContractEvents(
	ctx context.Context,
	logger *zap.Logger,
	executionClient executionclient.Provider,
	validatorCtrl validator.Controller,
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
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
	)
	if err != nil {
		logger.Fatal("failed to setup event data handler", zap.Error(err))
	}

	eventSyncer := eventsyncer.New(
		nodeStorage,
		executionClient,
		eventHandler,
		eventsyncer.WithLogger(logger),
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

func startMetricsHandler(logger *zap.Logger, db basedb.Database, port int, enableProf bool, opNode *operator.Node) {
	logger = logger.Named(logging.NameMetricsHandler)
	// init and start HTTP handler
	metricsHandler := metrics.NewHandler(db, enableProf, opNode)
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(logger, http.NewServeMux(), addr); err != nil {
		logger.Panic("failed to serve metrics", zap.Error(err))
	}
}

func initSlotPruning(ctx context.Context, logger *zap.Logger, stores *ibftstorage.ParticipantStores, slotTickerProvider slotticker.Provider, slot phase0.Slot, retain uint64) {
	var wg sync.WaitGroup

	threshold := slot - phase0.Slot(retain)

	// async perform initial slot gc
	_ = stores.Each(func(_ spectypes.BeaconRole, store qbftstorage.ParticipantStore) error {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Prune(ctx, logger, threshold)
		}()
		return nil
	})

	wg.Wait()

	// start background job for removing old slots on every tick
	_ = stores.Each(func(_ spectypes.BeaconRole, store qbftstorage.ParticipantStore) error {
		go store.PruneContinously(ctx, logger, slotTickerProvider, phase0.Slot(retain))
		return nil
	})
}
