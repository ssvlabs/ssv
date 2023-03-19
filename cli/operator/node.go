package operator

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/decided"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	"github.com/bloxapp/ssv/network"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/network/records"
	"github.com/bloxapp/ssv/operator"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options         `yaml:"db"`
	SSVOptions                 operator.Options       `yaml:"ssv"`
	ETH1Options                eth1.Options           `yaml:"eth1"`
	ETH2Options                beaconprotocol.Options `yaml:"eth2"`
	P2pNetworkConfig           p2pv1.Config           `yaml:"p2p"`

	OperatorPrivateKey         string `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key, used to decrypt contract events"`
	GenerateOperatorPrivateKey bool   `yaml:"GenerateOperatorPrivateKey" env:"GENERATE_OPERATOR_KEY" env-description:"Whether to generate operator key if none is passed by config"`
	MetricsAPIPort             int    `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
	EnableProfile              bool   `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	NetworkPrivateKey          string `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`

	WsAPIPort int  `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"port of WS API"`
	WithPing  bool `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`

	LocalEventsPath string `yaml:"LocalEventsPath" env:"EVENTS_PATH" env-description:"path to local events"`
}

var cfg config

var globalArgs global_config.Args

var operatorNode operator.Node

// StartNodeCmd is the command to start SSV node
var StartNodeCmd = &cobra.Command{
	Use:   "start-node",
	Short: "Starts an instance of SSV node",
	Run: func(cmd *cobra.Command, args []string) {
		logger, err := setupGlobal(cmd)
		if err != nil {
			log.Fatal("could not create logger", err)
		}

		logger.Info("maxProcs", zap.Int("maxProcs", runtime.GOMAXPROCS(0)))

		eth2Network, forkVersion := setupSSVNetwork(logger)

		cfg.DBOptions.Ctx = cmd.Context()
		db, err := setupDb(logger, eth2Network)
		if err != nil {
			logger.Fatal("could not setup db", zap.Error(err))
		}
		nodeStorage, operatorData := setupOperatorStorage(logger, db)

		keyManager, err := ekm.NewETHKeyManagerSigner(db, eth2Network, types.GetDefaultDomain(), logger)
		if err != nil {
			logger.Fatal("could not create new eth-key-manager signer", zap.Error(err))
		}

		cfg.P2pNetworkConfig.Ctx = cmd.Context()
		p2pNetwork := setupP2P(forkVersion, operatorData, db, logger)

		cfg.ETH2Options.Context = cmd.Context()
		el, cl := setupNodes(logger)

		ctx := cmd.Context()
		cfg.SSVOptions.ForkVersion = forkVersion
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.Beacon = el
		cfg.SSVOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.ForkVersion = forkVersion
		cfg.SSVOptions.ValidatorOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNetwork
		cfg.SSVOptions.ValidatorOptions.Beacon = el
		cfg.SSVOptions.ValidatorOptions.KeyManager = keyManager
		cfg.SSVOptions.ValidatorOptions.CleanRegistryData = cfg.ETH1Options.CleanRegistryData

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = nodeStorage.GetPrivateKey
		cfg.SSVOptions.ValidatorOptions.OperatorData = operatorData
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage

		cfg.SSVOptions.Eth1Client = cl

		if cfg.WsAPIPort != 0 {
			ws := api.NewWsServer(cmd.Context(), nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(logger, ws)
		}

		cfg.SSVOptions.ValidatorOptions.DutyRoles = []spectypes.BeaconRole{spectypes.BNRoleAttester} // TODO could be better to set in other place
		validatorCtrl := validator.NewController(logger, cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl

		operatorNode = operator.New(logger, cfg.SSVOptions)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), logger, db, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		metrics.WaitUntilHealthy(logger, cfg.SSVOptions.Eth1Client, "execution client")
		metrics.WaitUntilHealthy(logger, cfg.SSVOptions.Beacon, "consensus client")
		metrics.ReportSSVNodeHealthiness(true)

		// load & parse local events yaml if exists, otherwise sync from contract
		if len(cfg.LocalEventsPath) > 0 {
			if err := validator.LoadLocalEvents(
				logger,
				validatorCtrl.Eth1EventHandler(logger, false),
				cfg.LocalEventsPath,
			); err != nil {
				logger.Fatal("failed to load local events", zap.Error(err))
			}
		} else {
			if err := operatorNode.StartEth1(logger, eth1.StringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
				logger.Fatal("failed to start eth1", zap.Error(err))
			}
		}

		cfg.P2pNetworkConfig.GetValidatorStats = func() (uint64, uint64, uint64, error) {
			return validatorCtrl.GetValidatorStats(logger)
		}
		if err := p2pNetwork.Setup(logger); err != nil {
			logger.Fatal("failed to setup network", zap.Error(err))
		}
		if err := p2pNetwork.Start(logger); err != nil {
			logger.Fatal("failed to start network", zap.Error(err))
		}
		if err := operatorNode.Start(logger); err != nil {
			logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func setupGlobal(cmd *cobra.Command) (*zap.Logger, error) {
	commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)
	log.Printf("starting %s", commons.GetBuildData())
	if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
		return nil, fmt.Errorf("could not read config: %w", err)
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
			return nil, fmt.Errorf("could not read share config: %W", err)
		}
	}

	if err := logging.SetGlobalLogger(cfg.LogLevel, cfg.LogLevelFormat, cfg.LogFormat); err != nil {
		return nil, fmt.Errorf("logging.SetGlobalLogger: %w", err)
	}

	return zap.L(), nil
}

func setupDb(logger *zap.Logger, eth2Network beaconprotocol.Network) (basedb.IDb, error) {
	db, err := storage.GetStorageFactory(logger, cfg.DBOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open db")
	}
	reopenDb := func() error {
		if err := db.Close(logger); err != nil {
			return errors.Wrap(err, "failed to close db")
		}
		db, err = storage.GetStorageFactory(logger, cfg.DBOptions)
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
	if _, ok := db.(basedb.GarbageCollector); ok {
		// Close & reopen the database to trigger any unknown internal
		// startup/shutdown procedures that the storage engine may have.
		start := time.Now()
		if err := reopenDb(); err != nil {
			return nil, err
		}

		// Run a long garbage collection cycle with a timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if err := db.(basedb.GarbageCollector).FullGC(ctx); err != nil {
			return nil, errors.Wrap(err, "failed to collect garbage")
		}

		// Close & reopen again.
		if err := reopenDb(); err != nil {
			return nil, err
		}
		logger.Info("post-migrations garbage collection completed", zap.Duration("duration", time.Since(start)))
	}

	return db, nil
}

func setupOperatorStorage(logger *zap.Logger, db basedb.IDb) (operatorstorage.Storage, *registrystorage.OperatorData) {
	nodeStorage := operatorstorage.NewNodeStorage(db)
	operatorPubKey, err := nodeStorage.SetupPrivateKey(logger, cfg.OperatorPrivateKey, cfg.GenerateOperatorPrivateKey)
	if err != nil {
		logger.Fatal("could not setup operator private key", zap.Error(err))
	}

	_, found, err := nodeStorage.GetPrivateKey()
	if err != nil || !found {
		logger.Fatal("failed to get operator private key", zap.Error(err))
	}
	var operatorData *registrystorage.OperatorData
	operatorData, found, err = nodeStorage.GetOperatorDataByPubKey(logger, operatorPubKey)
	if err != nil {
		logger.Fatal("could not get operator data by public key", zap.Error(err))
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: operatorPubKey,
		}
	}

	return nodeStorage, operatorData
}

func setupSSVNetwork(logger *zap.Logger) (beaconprotocol.Network, forksprotocol.ForkVersion) {
	if len(cfg.P2pNetworkConfig.NetworkID) == 0 {
		cfg.P2pNetworkConfig.NetworkID = format.DomainType(types.GetDefaultDomain()).String()
	} else {
		// we have some custom network id, overriding default domain
		domainType, err := format.DomainTypeFromString(cfg.P2pNetworkConfig.NetworkID)
		if err != nil {
			logger.Fatal("failed to parse network id", zap.Error(err))
		}
		types.SetDefaultDomain(spectypes.DomainType(domainType))
	}
	eth2Network := beaconprotocol.NewNetwork(core.NetworkFromString(cfg.ETH2Options.Network), cfg.ETH2Options.MinGenesisTime)

	currentEpoch := eth2Network.EstimatedCurrentEpoch()
	forkVersion := forksprotocol.GetCurrentForkVersion(currentEpoch)

	logger.Info("setting ssv network", zap.String("domain", format.DomainType(types.GetDefaultDomain()).String()),
		zap.String("net-id", cfg.P2pNetworkConfig.NetworkID),
		zap.String("fork", string(forkVersion)))
	return eth2Network, forkVersion
}

func setupP2P(forkVersion forksprotocol.ForkVersion, operatorData *registrystorage.OperatorData, db basedb.IDb, logger *zap.Logger) network.P2PNetwork {
	istore := ssv_identity.NewIdentityStore(db)
	netPrivKey, err := istore.SetupNetworkKey(logger, cfg.NetworkPrivateKey)
	if err != nil {
		logger.Fatal("failed to setup network private key", zap.Error(err))
	}

	cfg.P2pNetworkConfig.NodeStorage = operatorstorage.NewNodeStorage(db)
	if len(cfg.P2pNetworkConfig.Subnets) == 0 {
		subnets := getNodeSubnets(logger, cfg.P2pNetworkConfig.NodeStorage.GetFilteredShares, forkVersion, operatorData.ID)
		cfg.P2pNetworkConfig.Subnets = subnets.String()
	}

	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey
	cfg.P2pNetworkConfig.ForkVersion = forkVersion
	cfg.P2pNetworkConfig.OperatorID = format.OperatorID(operatorData.PublicKey)
	cfg.P2pNetworkConfig.FullNode = cfg.SSVOptions.ValidatorOptions.FullNode

	return p2pv1.New(logger, &cfg.P2pNetworkConfig)
}

func setupNodes(logger *zap.Logger) (beaconprotocol.Beacon, eth1.Client) {
	// consensus client
	cfg.ETH2Options.Graffiti = []byte("SSV.Network")
	cl, err := goclient.New(logger, cfg.ETH2Options)
	if err != nil {
		logger.Fatal("failed to create beacon go-client", zap.Error(err),
			zap.String("addr", cfg.ETH2Options.BeaconNodeAddr))
	}

	// execution client
	logger.Info("using registry contract address", fields.Address(cfg.ETH1Options.RegistryContractAddr), zap.String("abi version", cfg.ETH1Options.AbiVersion.String()))
	if len(cfg.ETH1Options.RegistryContractABI) > 0 {
		logger.Info("using registry contract abi", zap.String("abi", cfg.ETH1Options.RegistryContractABI))
		if err = eth1.LoadABI(logger, cfg.ETH1Options.RegistryContractABI); err != nil {
			logger.Fatal("failed to load ABI JSON", zap.Error(err))
		}
	}
	el, err := goeth.NewEth1Client(logger, goeth.ClientOptions{
		Ctx:                  cfg.ETH2Options.Context,
		NodeAddr:             cfg.ETH1Options.ETH1Addr,
		ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
		ContractABI:          eth1.ContractABI(cfg.ETH1Options.AbiVersion),
		RegistryContractAddr: cfg.ETH1Options.RegistryContractAddr,
		AbiVersion:           cfg.ETH1Options.AbiVersion,
	})
	if err != nil {
		logger.Fatal("failed to create eth1 client", zap.Error(err))
	}

	return cl, el
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, db basedb.IDb, port int, enableProf bool) {
	logger = logger.Named(logging.NameMetricsHandler)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, db, enableProf, operatorNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(logger, http.NewServeMux(), addr); err != nil {
		// TODO: stop node if metrics setup failed?
		logger.Error("failed to start", zap.Error(err))
	}
}

// getNodeSubnets reads all shares and calculates the subnets for this node
// note that we'll trigger another update once finished processing registry events
func getNodeSubnets(
	logger *zap.Logger,
	getFiltered registrystorage.FilteredSharesFunc,
	ssvForkVersion forksprotocol.ForkVersion,
	operatorID spectypes.OperatorID,
) records.Subnets {
	f := forksfactory.NewFork(ssvForkVersion)
	subnetsMap := make(map[int]bool)
	shares, err := getFiltered(logger, registrystorage.ByOperatorIDAndNotLiquidated(operatorID))
	if err != nil {
		logger.Warn("could not read validators to bootstrap subnets")
		return nil
	}
	for _, share := range shares {
		subnet := f.ValidatorSubnet(hex.EncodeToString(share.ValidatorPubKey))
		if subnet < 0 {
			continue
		}
		if !subnetsMap[subnet] {
			subnetsMap[subnet] = true
		}
	}
	subnets := make([]byte, f.Subnets())
	for subnet := range subnetsMap {
		subnets[subnet] = 1
	}
	return subnets
}
