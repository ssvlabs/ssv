package exporter

import (
	"crypto/rsa"
	"fmt"
	global_config "github.com/bloxapp/ssv/cli/config"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/goeth"
	"github.com/bloxapp/ssv/exporter"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/api/adapters/gorilla"
	"github.com/bloxapp/ssv/metrics"
	metrics_ps "github.com/bloxapp/ssv/metrics/process"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"log"
	"net/http"
)

type config struct {
	global_config.GlobalConfig `yaml:"global"`
	DBOptions                  basedb.Options `yaml:"db"`
	P2pNetworkConfig           p2p.Config     `yaml:"p2p"`
	ETH1Options                eth1.Options   `yaml:"eth1"`

	WsAPIPort      int `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-default:"14000" env-description:"port of exporter WS api"`
	MetricsAPIPort int `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"port of metrics api"`
}

var cfg config

var globalArgs global_config.Args

// StartExporterNodeCmd is the command to start SSV boot node
var StartExporterNodeCmd = &cobra.Command{
	Use:   "start-exporter",
	Short: "Starts exporter node",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, &cfg); err != nil {
			log.Fatal(err)
		}
		// configure logger and db
		loggerLevel, err := logex.GetLoggerLevelValue(cfg.LogLevel)
		Logger := logex.Build(cmd.Parent().Short, loggerLevel, cfg.GlobalConfig.LogFormat)
		if err != nil {
			Logger.Warn(fmt.Sprintf("Default log level set to %s", loggerLevel), zap.Error(err))
		}
		cfg.DBOptions.Logger = Logger
		db, err := storage.GetStorageFactory(cfg.DBOptions)
		if err != nil {
			Logger.Fatal("failed to create db!", zap.Error(err))
		}

		network, err := p2p.New(cmd.Context(), Logger, &cfg.P2pNetworkConfig)
		if err != nil {
			Logger.Fatal("failed to create network", zap.Error(err))
		}

		Logger.Info("using registry contract address", zap.String("addr", cfg.ETH1Options.RegistryContractAddr))
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
			ContractABI:          eth1.ContractABI(),
			ConnectionTimeout:    cfg.ETH1Options.ETH1ConnectionTimeout,
			RegistryContractAddr: cfg.ETH1Options.RegistryContractAddr,
			// using an empty private key provider
			// because the exporter doesn't run in the context of an operator
			ShareEncryptionKeyProvider: func() (*rsa.PrivateKey, error) {
				return nil, nil
			},
		})
		if err != nil {
			Logger.Fatal("failed to create eth1 client", zap.Error(err))
		}

		exporterOptions := new(exporter.Options)
		exporterOptions.Eth1Client = eth1Client
		exporterOptions.Logger = Logger
		exporterOptions.Network = network
		exporterOptions.DB = db
		exporterOptions.Ctx = cmd.Context()
		exporterOptions.WS = api.NewWsServer(Logger, gorilla.NewGorillaAdapter(Logger), http.NewServeMux())
		exporterOptions.WsAPIPort = cfg.WsAPIPort

		exporterNode := exporter.New(*exporterOptions)

		if err := exporterNode.StartEth1(eth1.HexStringToSyncOffset(cfg.ETH1Options.ETH1SyncOffset)); err != nil {
			Logger.Fatal("failed to start eth1", zap.Error(err))
		}
		if cfg.MetricsAPIPort > 0 {
			go startMetricsHandler(Logger, network, cfg.MetricsAPIPort)
		}
		if err := exporterNode.Start(); err != nil {
			Logger.Fatal("failed to start exporter", zap.Error(err))
		}
	},
}

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartExporterNodeCmd)
}

func startMetricsHandler(logger *zap.Logger, net network.Network, port int) {
	// register process metrics
	metrics_ps.SetupProcessMetrics()
	p2p.SetupNetworkMetrics(logger, net)
	// init and start HTTP handler
	metricsHandler := metrics.NewMetricsHandler(logger)
	addr := fmt.Sprintf(":%d", port)
	logger.Info("starting metrics handler", zap.String("addr", addr))
	if err := metricsHandler.Start(http.NewServeMux(), addr); err != nil {
		logger.Error("failed to start metrics handler", zap.Error(err))
	}
}
