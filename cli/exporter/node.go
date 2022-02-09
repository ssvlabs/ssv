package exporter

import (
	"context"
	"crypto/rsa"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/beacon/goclient"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/migrations"
	"github.com/bloxapp/ssv/monitoring/metrics"
	networkForkV0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/commons"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"net/http"
	"time"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options `yaml:"db"`
	P2pNetworkConfig           p2p.Config     `yaml:"p2p"`
	ETH1Options                eth1.Options   `yaml:"eth1"`
	ETH2Options                beacon.Options `yaml:"eth2"`

	WsAPIPort                       int           `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-default:"14000" env-description:"port of exporter WS api"`
	WithPing                        bool          `yaml:"WithPing" env:"WITH_PING" env-description:"Whether to send websocket ping messages'"`
	MetricsAPIPort                  int           `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
	EnableProfile                   bool          `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"flag that indicates whether go profiling tools are enabled"`
	IbftSyncEnabled                 bool          `yaml:"IbftSyncEnabled" env:"IBFT_SYNC_ENABLED" env-default:"false" env-description:"enable ibft sync for all topics"`
	ValidatorMetaDataUpdateInterval time.Duration `yaml:"ValidatorMetaDataUpdateInterval" env:"VALIDATOR_METADATA_UPDATE_INTERVAL" env-default:"12m" env-description:"set the interval at which validator metadata gets updated"`
	NetworkPrivateKey               string        `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"private key for network identity"`
}

var cfg config

var globalArgs global_config.Args

var exporterNode exporter.Exporter

// StartExporterNodeCmd is the command to start SSV boot node
var StartExporterNodeCmd = &cobra.Command{
	Use:   "start-exporter",
	Short: "Starts exporter node",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		if globalArgs.ShareConfigPath != "" {
			if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, &cfg); err != nil {
				log.Fatal(err)
			}
		}
		// configure logger and db
		commons.SetBuildData(cmd.Parent().Short, cmd.Parent().Version)
		log.Printf("starting %s", commons.GetBuildData())
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

		cfg.P2pNetworkConfig.NetworkPrivateKey, err = utils.ECDSAPrivateKey(Logger.With(zap.String("who", "p2pNetworkPrivateKey")), cfg.NetworkPrivateKey)
		if err != nil {
			log.Fatal("Failed to get p2p privateKey", zap.Error(err))
		}
		cfg.P2pNetworkConfig.ReportLastMsg = true
		// TODO add fork interface for exporter or use the same forks as in operator
		cfg.P2pNetworkConfig.Fork = networkForkV0.New()
		cfg.P2pNetworkConfig.NodeType = p2p.Exporter
		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		Logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr), zap.String("abiVersion", cfg.ETH1Options.AbiVersion.String()))

		if len(cfg.ETH1Options.RegistryContractABI) > 0 {
			Logger.Info("using registry contract abi", zap.String("abi", cfg.ETH1Options.RegistryContractABI))
			if err = eth1.LoadABI(cfg.ETH1Options.RegistryContractABI); err != nil {
				Logger.Fatal("failed to load ABI JSON", zap.Error(err))
			}
		}
		eth1Client, err := goeth.NewEth1Client(goeth.ClientOptions{
			Ctx:                  cmd.Context(),
			Logger:               Logger,
			NodeAddr:             cfg.ETH1Options.ETH1Addr,
			ContractABI:          eth1.ContractABI(cfg.ETH1Options.AbiVersion),
			ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
			RegistryContractAddr: cfg.ETH1Options.RegistryContractAddr,
			// using an empty private key provider
			// because the exporter doesn't run in the context of an operator
			ShareEncryptionKeyProvider: func() (*rsa.PrivateKey, bool, error) {
				return nil, true, nil
			},
			AbiVersion: cfg.ETH1Options.AbiVersion,
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		// TODO Not refactored yet Start
		cfg.ETH2Options.Context = cmd.Context()
		cfg.ETH2Options.Logger = Logger
		cfg.ETH2Options.Graffiti = []byte("SSV.Network")
		cfg.ETH2Options.DB = db
		beaconClient, err := goclient.New(cfg.ETH2Options)
		if err != nil {
			Logger.Fatal("failed to create beacon go-client", zap.Error(err))
		}

		exporterOptions := new(exporter.Options)
		exporterOptions.Eth1Client = eth1Client
		exporterOptions.Beacon = beaconClient
		exporterOptions.Logger = Logger
		exporterOptions.Network = network
		exporterOptions.DB = db
		exporterOptions.Ctx = cmd.Context()
		exporterOptions.WS = api.NewWsServer(cmd.Context(), Logger, nil, http.NewServeMux(), cfg.WithPing)
		exporterOptions.WsAPIPort = cfg.WsAPIPort
		exporterOptions.IbftSyncEnabled = cfg.IbftSyncEnabled
		exporterOptions.CleanRegistryData = cfg.ETH1Options.CleanRegistryData
		exporterOptions.ValidatorMetaDataUpdateInterval = cfg.ValidatorMetaDataUpdateInterval
		exporterOptions.UseMainTopic = cfg.P2pNetworkConfig.UseMainTopic

		exporterNode = exporter.New(*exporterOptions)

		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(cmd.Context(), Logger, cfg.MetricsAPIPort, cfg.EnableProfile)
		}

		metrics.WaitUntilHealthy(Logger, eth1Client, "eth1 node")
		metrics.WaitUntilHealthy(Logger, beaconClient, "beacon node")

		if err := exporterNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if err := exporterNode.Start(); err != nil {
			Logger.Fatal("failed to start exporter", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartExporterNodeCmd)
}

func startMetricsHandler(ctx context.Context, logger *zap.Logger, port int, enableProf bool) {
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(ctx, logger, enableProf, exporterNode.(metrics.HealthCheckAgent))
	addr := fmt.Sprintf(":%d", port)
	logger.Info("starting metrics handler", zap.String("addr", addr))
	if err := metricsHandler.Start(http.NewServeMux(), addr); err != nil {
		logger.Error("failed to start metrics handler", zap.Error(err))
	}
}
