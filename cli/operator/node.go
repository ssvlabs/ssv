package operator

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/decided"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/records"
	"log"
	"net/http"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/ilyakaznacheev/cleanenv"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/time/slots"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	ssv_identity "github.com/bloxapp/ssv/identity"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	forksv0 "github.com/bloxapp/ssv/network/forks/v0"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/operator"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/bloxapp/ssv/utils/rsaencryption"
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

	ForkV1Epoch uint64 `yaml:"ForkV1Epoch" env:"FORKV1_EPOCH" env-default:"102594" env-description:"Target epoch for fork v1"`
	ForkV2Epoch uint64 `yaml:"ForkV2Epoch" env:"FORKV2_EPOCH" env-default:"102595" env-description:"Target epoch for fork v2"`

	WsAPIPort int  `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"port of WS API"`
	WithPing  bool `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`
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
		log.Printf("starting %s", commons.GetBuildData())
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatalf("could not read config %s", err)
		}
		if globalArgs.ShareConfigPath != "" {
			if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
				log.Fatalf("could not read share config %s", err)
			}
		}
		loggerLevel, errLogLevel := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(commons.GetBuildData(), loggerLevel, &logex.EncodingConfig{
			Format:       cfg.GlobalConfig.LogFormat,
			LevelEncoder: logex.LevelEncoder([]byte(cfg.LogLevelFormat)),
		})
		if errLogLevel != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(errLogLevel))
		}

		cfg.DBOptions.Logger = Logger
		cfg.DBOptions.Ctx = cmd.Context()
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		migrationOpts := migrations.Options{
			Db:     db,
			Logger: Logger,
			DbPath: cfg.DBOptions.Path,
		}
		err = migrations.Run(cmd.Context(), migrationOpts)
		if err != nil {
			Logger.Fatal("failed to run migrations", zap.Error(err))
		}

		eth2Network := beaconprotocol.NewNetwork(core.NetworkFromString(cfg.ETH2Options.Network))

		currentEpoch := slots.EpochsSinceGenesis(time.Unix(int64(eth2Network.MinGenesisTime()), 0))
		if cfg.ForkV1Epoch > 0 {
			forksprotocol.SetForkEpoch(types.Epoch(cfg.ForkV1Epoch), forksprotocol.V1ForkVersion)
		}
		if cfg.ForkV2Epoch > 0 {
			Logger.Debug("setting v2 epoch", zap.Uint64("epoch", cfg.ForkV2Epoch))
			forksprotocol.SetForkEpoch(types.Epoch(cfg.ForkV2Epoch), forksprotocol.V2ForkVersion)
		}
		ssvForkVersion := forksprotocol.GetCurrentForkVersion(currentEpoch)
		Logger.Info("using ssv fork version", zap.String("version", string(ssvForkVersion)))
		// TODO Not refactored yet Start (refactor in exporter as well):
		cfg.ETH2Options.Context = cmd.Context()
		cfg.ETH2Options.Logger = Logger
		cfg.ETH2Options.Graffiti = []byte("SSV.Network")
		cfg.ETH2Options.DB = db
		beaconClient, err := goclient.New(cfg.ETH2Options)
		if err != nil {
			Logger.Fatal("failed to create beacon go-client", zap.Error(err),
				zap.String("addr", cfg.ETH2Options.BeaconNodeAddr))
		}

		nodeStorage := operatorstorage.NewNodeStorage(db, Logger)
		if err := nodeStorage.SetupPrivateKey(cfg.GenerateOperatorPrivateKey, cfg.OperatorPrivateKey); err != nil {
			Logger.Fatal("failed to setup operator private key", zap.Error(err))
		}
		operatorPrivateKey, found, err := nodeStorage.GetPrivateKey()
		if err != nil || !found {
			Logger.Fatal("failed to get operator private key", zap.Error(err))
		}
		operatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivateKey)
		if err != nil {
			Logger.Fatal("failed to extract operator public key", zap.Error(err))
		}

		istore := ssv_identity.NewIdentityStore(db, Logger)
		netPrivKey, err := istore.SetupNetworkKey(cfg.NetworkPrivateKey)
		if err != nil {
			Logger.Fatal("failed to setup network private key", zap.Error(err))
		}

		if len(cfg.P2pNetworkConfig.Subnets) == 0 {
			subnets := getNodeSubnets(Logger, db, ssvForkVersion, operatorPubKey)
			cfg.P2pNetworkConfig.Subnets = subnets.String()
		}

		cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey
		cfg.P2pNetworkConfig.Logger = Logger
		cfg.P2pNetworkConfig.ForkVersion = ssvForkVersion
		cfg.P2pNetworkConfig.OperatorID = format.OperatorID(operatorPubKey)
		cfg.P2pNetworkConfig.UserAgent = forksv0.GenUserAgentWithOperatorID(cfg.P2pNetworkConfig.OperatorID)
		//Logger.Info("xxx", zap.String("ua", cfg.P2pNetworkConfig.UserAgent), zap.String("oid", cfg.P2pNetworkConfig.OperatorID))
		p2pNet := p2pv1.New(cmd.Context(), &cfg.P2pNetworkConfig)
		if err := p2pNet.Setup(); err != nil {
			Logger.Fatal("failed to setup network", zap.Error(err))
		}

		ctx := cmd.Context()
		cfg.SSVOptions.ForkVersion = ssvForkVersion
		cfg.SSVOptions.Context = ctx
		cfg.SSVOptions.Logger = Logger
		cfg.SSVOptions.DB = db
		cfg.SSVOptions.Beacon = beaconClient
		cfg.SSVOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.Network = p2pNet
		cfg.SSVOptions.ValidatorOptions.ForkVersion = ssvForkVersion
		cfg.SSVOptions.ValidatorOptions.ETHNetwork = eth2Network
		cfg.SSVOptions.ValidatorOptions.Logger = Logger
		cfg.SSVOptions.ValidatorOptions.Context = ctx
		cfg.SSVOptions.ValidatorOptions.DB = db
		cfg.SSVOptions.ValidatorOptions.Network = p2pNet
		cfg.SSVOptions.ValidatorOptions.Beacon = beaconClient
		cfg.SSVOptions.ValidatorOptions.CleanRegistryData = cfg.ETH1Options.CleanRegistryData
		cfg.SSVOptions.ValidatorOptions.KeyManager = beaconClient

		cfg.SSVOptions.ValidatorOptions.ShareEncryptionKeyProvider = nodeStorage.GetPrivateKey
		cfg.SSVOptions.ValidatorOptions.OperatorPubKey = operatorPubKey
		cfg.SSVOptions.ValidatorOptions.RegistryStorage = nodeStorage

		Logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr), zap.String("abi version", cfg.ETH1Options.AbiVersion.String()))

		// create new eth1 client
		if len(cfg.ETH1Options.RegistryContractABI) > 0 {
			Logger.Info("using registry contract abi", zap.String("abi", cfg.ETH1Options.RegistryContractABI))
			if err = eth1.LoadABI(cfg.ETH1Options.RegistryContractABI); err != nil {
				Logger.Fatal("failed to load ABI JSON", zap.Error(err))
			}
		}
		cfg.SSVOptions.Eth1Client, err = goeth.NewEth1Client(goeth.ClientOptions{
			Ctx:                  cmd.Context(),
			Logger:               Logger,
			NodeAddr:             cfg.ETH1Options.ETH1Addr,
			ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
			ContractABI:          eth1.ContractABI(cfg.ETH1Options.AbiVersion),
			RegistryContractAddr: cfg.ETH1Options.RegistryContractAddr,
			AbiVersion:           cfg.ETH1Options.AbiVersion,
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		if cfg.WsAPIPort != 0 {
			ws := api.NewWsServer(cmd.Context(), Logger, nil, http.NewServeMux(), cfg.WithPing)
			cfg.SSVOptions.WS = ws
			cfg.SSVOptions.WsAPIPort = cfg.WsAPIPort
			cfg.SSVOptions.ValidatorOptions.NewDecidedHandler = decided.NewStreamPublisher(Logger, ws)
		}

		validatorCtrl := validator.NewController(cfg.SSVOptions.ValidatorOptions)
		cfg.SSVOptions.ValidatorController = validatorCtrl

		operatorNode = operator.New(cfg.SSVOptions)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), Logger, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		metrics.WaitUntilHealthy(Logger, cfg.SSVOptions.Eth1Client, "eth1 node")
		metrics.WaitUntilHealthy(Logger, beaconClient, "beacon node")

		if err := operatorNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if err := p2pNet.Start(); err != nil {
			Logger.Fatal("failed to start network", zap.Error(err))
		}
		if err := operatorNode.Start(); err != nil {
			Logger.Fatal("failed to start SSV node", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, port int, enableProf bool) {
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, logger, enableProf, operatorNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	if err := metricsHandler.Start(http.NewServeMux(), addr); err != nil {
		// TODO: stop node if metrics setup failed?
		logger.Error("failed to start metrics handler", zap.Error(err))
	}
}

// getNodeSubnets reads all shares and calculates the subnets for this node
// note that we'll trigger another update once finished processing registry events
func getNodeSubnets(logger *zap.Logger, db basedb.IDb, ssvForkVersion forksprotocol.ForkVersion, operatorPubKey string) records.Subnets {
	f := forksfactory.NewFork(ssvForkVersion)
	sharesStorage := validator.NewCollection(validator.CollectionOptions{
		DB:     db,
		Logger: logger,
	})
	subnetsMap := make(map[int]bool)
	shares, err := sharesStorage.GetOperatorValidatorShares(operatorPubKey, true)
	if err != nil {
		logger.Warn("could not read validators to bootstrap subnets")
		return nil
	}
	for _, share := range shares {
		subnet := f.ValidatorSubnet(share.PublicKey.SerializeToHexStr())
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
